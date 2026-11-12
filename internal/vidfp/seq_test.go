package vidfp

import "testing"

func TestSeqCodecRoundTrip(t *testing.T) {
	s := Seq{Duration: 12.5, Frames: []SeqFrame{{T: 1, H: 2}, {T: 3.5, H: ^uint64(0)}}}
	got, ok := DecodeSeq(s.Encode())
	if !ok || got.Duration != s.Duration || len(got.Frames) != 2 || got.Frames[1] != s.Frames[1] {
		t.Fatalf("round trip = %+v %v", got, ok)
	}
	if _, ok := DecodeSeq([]byte{1, 2, 3}); ok {
		t.Fatal("short input must fail")
	}
}

func TestPictureBoxFindsLetterbox(t *testing.T) {
	const n = 8
	raw := make([]byte, n*seqFrame)
	for i := range n {
		for y := 10; y < 54; y++ { // bars above and below
			for x := range seqSide {
				raw[i*seqFrame+y*seqSide+x] = byte(100 + (x+y+i)%50)
			}
		}
	}
	b := pictureBox(raw, n)
	if b.y0 != 10 || b.y1 != 54 || b.x0 != 0 || b.x1 != seqSide {
		t.Fatalf("box = %+v", b)
	}
}
