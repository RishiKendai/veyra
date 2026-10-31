package vidfp

import "testing"

func TestFPRoundTrip(t *testing.T) {
	f := FP{Info: 0xABCD, Distinct: 7, Duration: 123.456, Have: ProbeMask}
	for i := range f.P {
		f.P[i] = uint64(i) * 0x9E3779B97F4A7C15
	}
	got, ok := DecodeFP(f.Encode())
	if !ok || got != f {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	if _, ok := DecodeFP([]byte{1, 2}); ok {
		t.Error("short input must fail")
	}
}
