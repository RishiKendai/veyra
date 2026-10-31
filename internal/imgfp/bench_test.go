package imgfp

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// A 12-megapixel (4000x3000) JPEG, like a phone photo.
func BenchmarkComputeJPEG12MP(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	for y := range 3000 {
		for x := range 4000 {
			img.Set(x, y, color.RGBA{uint8(x / 16), uint8(y / 12), uint8((x + y) / 28), 255})
		}
	}
	p := filepath.Join(b.TempDir(), "big.jpg")
	f, _ := os.Create(p)
	jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
	f.Close()
	fi, _ := os.Stat(p)
	b.SetBytes(fi.Size())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Compute(p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashesFromThumb(b *testing.B) {
	t := thumbnail(scene(256, 256))
	b.ReportAllocs()
	b.ResetTimer() // exclude building the test scene
	for range b.N {
		_ = fromThumb(t)
	}
}
