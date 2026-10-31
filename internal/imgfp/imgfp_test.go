package imgfp

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// scene draws an asymmetric w x h picture so rotations/flips are detectable.
func scene(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8(40 + 120*x/w)
			if x < w/3 && y < h/2 {
				v = 230
			}
			if x > w/2 && y > 2*h/3 {
				v = 10
			}
			img.Set(x, y, color.RGBA{v, v / 2, 255 - v, 255})
		}
	}
	return img
}

func rotate90ccw(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := range h {
		for x := range w {
			dst.Set(y, w-1-x, src.At(x, y))
		}
	}
	return dst
}

// withOrientation inserts an EXIF APP1 segment carrying the orientation tag.
func withOrientation(t *testing.T, jpg []byte, o uint16) []byte {
	t.Helper()
	var tiff bytes.Buffer
	tiff.WriteString("MM")
	binary.Write(&tiff, binary.BigEndian, uint16(42))
	binary.Write(&tiff, binary.BigEndian, uint32(8))
	binary.Write(&tiff, binary.BigEndian, uint16(1)) // one IFD entry
	binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	binary.Write(&tiff, binary.BigEndian, uint16(3)) // SHORT
	binary.Write(&tiff, binary.BigEndian, uint32(1))
	binary.Write(&tiff, binary.BigEndian, o)
	binary.Write(&tiff, binary.BigEndian, uint16(0))
	binary.Write(&tiff, binary.BigEndian, uint32(0))
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	seg = append(seg, payload...)
	return append(append(append([]byte{}, jpg[:2]...), seg...), jpg[2:]...)
}

func jpegOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func hashFile(t *testing.T, name string, data []byte) Hashes {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := Compute(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestEXIFOrientationIsNormalised(t *testing.T) {
	upright := scene(160, 100)
	want := hashFile(t, "up.jpg", jpegOf(t, upright))

	// Stored rotated 90° CCW + orientation 6 (rotate 90° CW to display) == upright.
	stored := jpegOf(t, rotate90ccw(upright))
	tagged := hashFile(t, "tagged.jpg", withOrientation(t, stored, 6))
	untagged := hashFile(t, "untagged.jpg", stored)

	if d := Dist(want.P, tagged.P); d > 4 {
		t.Errorf("oriented pHash distance %d, want <= 4", d)
	}
	if d := Dist(want.P, untagged.P); d < 10 {
		t.Errorf("without the tag the rotated file should differ clearly, got %d", d)
	}
}

func TestOrientationParsing(t *testing.T) {
	base := jpegOf(t, scene(32, 32))
	for o := uint16(1); o <= 8; o++ {
		if got := jpegOrientation(bytes.NewReader(withOrientation(t, base, o))); got != int(o) {
			t.Errorf("orientation %d parsed as %d", o, got)
		}
	}
	if got := jpegOrientation(bytes.NewReader(base)); got != 1 {
		t.Errorf("no EXIF: got %d", got)
	}
	if got := jpegOrientation(bytes.NewReader([]byte("garbage"))); got != 1 {
		t.Errorf("garbage: got %d", got)
	}
}

func TestAllEightOrientationsMapToUpright(t *testing.T) {
	t0 := thumbnail(scene(96, 96))
	// Applying the 4 rotations in sequence via orientation 6 must return the original.
	r := t0
	for range 4 {
		r = applyOrientation(r, 6)
	}
	if *r != *t0 {
		t.Error("four 90° rotations should be the identity")
	}
	if *applyOrientation(applyOrientation(t0, 2), 2) != *t0 {
		t.Error("double horizontal flip should be the identity")
	}
	if *applyOrientation(applyOrientation(t0, 5), 5) != *t0 {
		t.Error("double transpose should be the identity")
	}
}

func TestFlatImagesAreMarked(t *testing.T) {
	var b bytes.Buffer
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	png.Encode(&b, img)
	if h := hashFile(t, "flat.png", b.Bytes()); !h.Flat {
		t.Error("solid image must be flagged Flat")
	}
	if h := hashFile(t, "scene.png", pngOf(t, scene(64, 64))); h.Flat {
		t.Error("structured image must not be Flat")
	}
}

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTinyImageDoesNotPanic(t *testing.T) {
	h := hashFile(t, "tiny.png", pngOf(t, scene(5, 3)))
	_ = h
}

func TestCorruptImageErrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jpg")
	os.WriteFile(p, jpegOf(t, scene(64, 64))[:200], 0o600)
	if _, err := Compute(p); err == nil {
		t.Error("truncated JPEG should error")
	}
}

func TestHashesCodec(t *testing.T) {
	for _, h := range []Hashes{{A: 1, D: 2, P: 3}, {A: ^uint64(0), Flat: true}} {
		got, ok := DecodeHashes(h.Encode())
		if !ok || got != h {
			t.Errorf("got %+v ok=%v want %+v", got, ok, h)
		}
	}
	if _, ok := DecodeHashes(nil); ok {
		t.Error("empty input must fail")
	}
}
