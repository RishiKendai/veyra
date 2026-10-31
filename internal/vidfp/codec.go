package vidfp

import (
	"encoding/binary"
	"math"
)

// CodecVersion identifies the binary layout and the sampling strategy; it
// belongs in the params string of any cache holding FP values. The seg/kf
// fields come from the removed keyframe tier and are kept so existing caches
// remain valid; bump the leading tag when the FP layout or sampling changes.
const CodecVersion = "vidfp2;k16;seg3;kf600"

const fpLen = K*8 + 4 + 4 + 8 + 4

// Encode serialises f for a persistent cache.
func (f FP) Encode() []byte {
	b := make([]byte, fpLen)
	o := 0
	for _, p := range f.P {
		binary.LittleEndian.PutUint64(b[o:], p)
		o += 8
	}
	binary.LittleEndian.PutUint32(b[o:], f.Info)
	binary.LittleEndian.PutUint32(b[o+4:], uint32(int32(f.Distinct)))
	binary.LittleEndian.PutUint64(b[o+8:], math.Float64bits(f.Duration))
	binary.LittleEndian.PutUint32(b[o+16:], f.Have)
	return b
}

// DecodeFP reverses FP.Encode. It reports false for malformed input.
func DecodeFP(b []byte) (FP, bool) {
	var f FP
	if len(b) != fpLen {
		return f, false
	}
	o := 0
	for i := range f.P {
		f.P[i] = binary.LittleEndian.Uint64(b[o:])
		o += 8
	}
	f.Info = binary.LittleEndian.Uint32(b[o:])
	f.Distinct = int(int32(binary.LittleEndian.Uint32(b[o+4:])))
	f.Duration = math.Float64frombits(binary.LittleEndian.Uint64(b[o+8:]))
	f.Have = binary.LittleEndian.Uint32(b[o+16:])
	return f, true
}
