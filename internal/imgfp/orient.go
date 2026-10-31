package imgfp

import (
	"bufio"
	"encoding/binary"
	"io"
)

// jpegOrientation returns the EXIF orientation (1..8) of a JPEG stream, or 1
// when absent or unreadable. It walks marker segments up to the scan data and
// never reads pixel data.
func jpegOrientation(r io.Reader) int {
	br := bufio.NewReader(r)
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return 1
	}
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 1
		}
		if b != 0xFF {
			continue
		}
		m, err := br.ReadByte()
		for err == nil && m == 0xFF { // fill bytes
			m, err = br.ReadByte()
		}
		if err != nil {
			return 1
		}
		switch {
		case m == 0x00 || m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			continue // standalone markers carry no length
		case m == 0xD9 || m == 0xDA:
			return 1 // end of image / start of scan: no EXIF seen
		}
		var l [2]byte
		if _, err := io.ReadFull(br, l[:]); err != nil {
			return 1
		}
		n := int(binary.BigEndian.Uint16(l[:])) - 2
		if n < 0 {
			return 1
		}
		if m == 0xE1 && n >= 14 {
			data := make([]byte, n)
			if _, err := io.ReadFull(br, data); err != nil {
				return 1
			}
			if string(data[:6]) == "Exif\x00\x00" {
				return tiffOrientation(data[6:])
			}
			continue
		}
		if _, err := br.Discard(n); err != nil {
			return 1
		}
	}
}

func tiffOrientation(d []byte) int {
	if len(d) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(d[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(d[4:8]))
	if ifd < 8 || ifd+2 > len(d) {
		return 1
	}
	count := int(bo.Uint16(d[ifd:]))
	for i := range count {
		e := ifd + 2 + 12*i
		if e+12 > len(d) {
			return 1
		}
		if bo.Uint16(d[e:]) == 0x0112 {
			if v := int(bo.Uint16(d[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
