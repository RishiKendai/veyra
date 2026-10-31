package imgfp

import (
	"encoding/binary"
	"fmt"
)

// CodecVersion identifies the Hashes binary layout and the algorithm that
// produced it; it belongs in the params string of any cache holding them.
var CodecVersion = fmt.Sprintf("imgfp1;n%d", N)

const encodedLen = 3*8 + 1

// Encode serialises h for a persistent cache.
func (h Hashes) Encode() []byte {
	b := make([]byte, encodedLen)
	binary.LittleEndian.PutUint64(b[0:], h.A)
	binary.LittleEndian.PutUint64(b[8:], h.D)
	binary.LittleEndian.PutUint64(b[16:], h.P)
	if h.Flat {
		b[24] = 1
	}
	return b
}

// DecodeHashes reverses Encode. It reports false for malformed input.
func DecodeHashes(b []byte) (Hashes, bool) {
	if len(b) != encodedLen {
		return Hashes{}, false
	}
	return Hashes{
		A:    binary.LittleEndian.Uint64(b[0:]),
		D:    binary.LittleEndian.Uint64(b[8:]),
		P:    binary.LittleEndian.Uint64(b[16:]),
		Flat: b[24] == 1,
	}, true
}
