// Package classify decides whether a file is really an image or video by
// inspecting its content. Extensions are never consulted.
package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/prof"
)

// ErrNotMedia means the file is not of the requested type. It is expected
// for most files in a directory and is not reported as a skip.
var ErrNotMedia = errors.New("not the requested media type")

// Any other error returned by a classifier means "looked like media but
// could not be used" and is reported to the user as a skip with a reason.

const (
	sniffLen     = 4096
	probeTimeout = 15 * time.Second
	maxPixels    = 400_000_000 // reject decompression-bomb dimensions
)

// Classifier classifies files for one media type.
type Classifier func(ctx context.Context, path string) (model.Meta, error)

// For returns the classifier for t. Video requires ffprobe; it returns an
// error when ffprobe is unavailable.
func For(t model.MediaType) (Classifier, error) {
	switch t {
	case model.Image:
		return Image, nil
	case model.Video:
		if _, err := exec.LookPath("ffprobe"); err != nil {
			return nil, errors.New("ffprobe not found in PATH (install with: brew install ffmpeg)")
		}
		return Video, nil
	}
	return nil, fmt.Errorf("unsupported media type %q", t)
}

func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, sniffLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

// Image accepts a file only if a registered decoder can read its header.
func Image(_ context.Context, path string) (model.Meta, error) {
	defer prof.Since("classify.image", prof.Start(), 0)
	head, err := readHead(path)
	if err != nil {
		return model.Meta{}, err
	}
	if len(head) < 12 {
		return model.Meta{}, ErrNotMedia
	}
	if isHEIF(head) {
		return model.Meta{}, errors.New("HEIC/HEIF/AVIF images are not supported yet")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return model.Meta{}, ErrNotMedia
		}
		// Magic matched but the header is truncated/corrupt, or lies beyond the sniff window.
		return reDecodeConfig(path)
	}
	return checkImage(cfg, format)
}

func checkImage(cfg image.Config, format string) (model.Meta, error) {
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return model.Meta{}, fmt.Errorf("image too large (%dx%d)", cfg.Width, cfg.Height)
	}
	return model.Meta{Format: format, Width: cfg.Width, Height: cfg.Height, ColorModel: colorModelName(cfg.ColorModel)}, nil
}

// reDecodeConfig retries against the whole file for formats whose headers
// may sit beyond the sniff window (e.g. large EXIF blocks before SOF).
func reDecodeConfig(path string) (model.Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.Meta{}, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return model.Meta{}, ErrNotMedia
		}
		return model.Meta{}, fmt.Errorf("corrupt %s image: %w", format, err)
	}
	return checkImage(cfg, format)
}

// colorModelName describes the decoder's colour model as the user would
// recognise it. The depth is that of the decoded representation.
func colorModelName(m color.Model) string {
	switch m {
	case color.YCbCrModel:
		return "YCbCr (8-bit)"
	case color.GrayModel:
		return "Gray (8-bit)"
	case color.Gray16Model:
		return "Gray (16-bit)"
	case color.RGBAModel, color.NRGBAModel:
		return "RGB(A) (8-bit)"
	case color.RGBA64Model, color.NRGBA64Model:
		return "RGB(A) (16-bit)"
	case color.CMYKModel:
		return "CMYK (8-bit)"
	}
	if _, ok := m.(color.Palette); ok {
		return "Indexed"
	}
	return ""
}

func isHEIF(b []byte) bool {
	if len(b) < 12 || string(b[4:8]) != "ftyp" {
		return false
	}
	switch string(b[8:12]) {
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1", "avif":
		return true
	}
	return false
}

// looksLikeVideoContainer is a cheap magic-byte prefilter so ffprobe only
// runs on plausible candidates. It is deliberately permissive.
func looksLikeVideoContainer(b []byte) bool {
	switch {
	case len(b) >= 12 && string(b[4:8]) == "ftyp": // mp4, mov, m4v, 3gp
		return !isHEIF(b)
	case len(b) >= 8 && string(b[4:8]) == "moov", len(b) >= 8 && string(b[4:8]) == "mdat",
		len(b) >= 8 && string(b[4:8]) == "free", len(b) >= 8 && string(b[4:8]) == "wide":
		return true // QuickTime without ftyp
	case bytes.HasPrefix(b, []byte{0x1A, 0x45, 0xDF, 0xA3}): // Matroska / WebM
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "AVI ":
		return true
	case bytes.HasPrefix(b, []byte("OggS")), bytes.HasPrefix(b, []byte("FLV")),
		bytes.HasPrefix(b, []byte{0x30, 0x26, 0xB2, 0x75}): // ASF/WMV
		return true
	case bytes.HasPrefix(b, []byte{0x00, 0x00, 0x01, 0xBA}), bytes.HasPrefix(b, []byte{0x00, 0x00, 0x01, 0xB3}): // MPEG PS/ES
		return true
	case len(b) >= 189*3 && b[0] == 0x47 && b[188] == 0x47 && b[376] == 0x47: // MPEG-TS
		return true
	case len(b) >= 193*3 && b[4] == 0x47 && b[192] == 0x47 && b[380] == 0x47: // M2TS
		return true
	}
	return false
}

type probeOut struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		Disposition  struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

// Video accepts a file only if ffprobe finds a real (non-cover-art,
// non-still-image) video stream.
func Video(ctx context.Context, path string) (model.Meta, error) {
	defer prof.Since("classify.video(ffprobe)", prof.Start(), 0)
	head, err := readHead(path)
	if err != nil {
		return model.Meta{}, err
	}
	if !looksLikeVideoContainer(head) {
		return model.Meta{}, ErrNotMedia
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,avg_frame_rate:stream_disposition=attached_pic:format=format_name,duration,bit_rate",
		"-of", "json", "-i", path)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return model.Meta{}, fmt.Errorf("ffprobe: %w", ctx.Err())
		}
		return model.Meta{}, fmt.Errorf("unreadable video: %s", firstLine(stderr.String(), err))
	}
	var out probeOut
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return model.Meta{}, fmt.Errorf("ffprobe output: %w", err)
	}
	format := out.Format.FormatName
	if strings.Contains(format, "_pipe") || format == "image2" || format == "gif" {
		return model.Meta{}, ErrNotMedia // still images / animated GIF are images, not videos
	}
	var meta model.Meta
	found := false
	for _, s := range out.Streams {
		switch {
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && !found:
			found = true
			meta = model.Meta{Format: format, Width: s.Width, Height: s.Height, Codec: s.CodecName, FrameRate: parseRate(s.AvgFrameRate)}
		case s.CodecType == "audio" && meta.AudioCodec == "":
			meta.AudioCodec = s.CodecName
		}
	}
	if !found {
		return model.Meta{}, ErrNotMedia
	}
	meta.Duration, _ = strconv.ParseFloat(out.Format.Duration, 64)
	meta.BitRate, _ = strconv.ParseInt(out.Format.BitRate, 10, 64)
	return meta, nil
}

// parseRate parses ffprobe rationals such as "30000/1001"; "0/0" means unknown.
func parseRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return n / d
}

func firstLine(s string, fallback error) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback.Error()
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
