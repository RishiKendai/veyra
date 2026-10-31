package format

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	cases := []struct{ got, want string }{
		{Size(0), "0 B"}, {Size(1023), "1023 B"}, {Size(1024), "1.0 KiB"}, {Size(1536), "1.5 KiB"},
		{Size(5 << 20), "5.0 MiB"}, {Size(3 << 40), "3.0 TiB"},
		{Int(0), "0"}, {Int(999), "999"}, {Int(1000), "1,000"}, {Int(2450), "2,450"}, {Int(1234567), "1,234,567"}, {Int(-12345), "-12,345"},
		{Duration(0), "Unknown"}, {Duration(-1), "Unknown"}, {Duration(135), "02:15"}, {Duration(3725), "1:02:05"}, {Duration(59.6), "01:00"},
		{Elapsed(850 * time.Millisecond), "850 ms"}, {Elapsed(12400 * time.Millisecond), "12.4 s"}, {Elapsed(151 * time.Second), "2m 31s"}, {Elapsed(125 * time.Minute), "2h 05m"}, {Elapsed(61 * time.Minute), "1h 01m"},
		{Bitrate(0), "Unknown"}, {Bitrate(640_000), "640 kb/s"}, {Bitrate(2_400_000), "2.4 Mb/s"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
