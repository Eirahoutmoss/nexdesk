package capture

import "testing"

func TestScaleBGRA(t *testing.T) {
	w, h := 1920, 1080
	raw := make([]byte, w*h*4)
	for i := 0; i < len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2] = 10, 20, 30 // B G R
	}
	full := scaleBGRA(raw, w, h, 0, false)
	if full.Bounds().Dx() != w || full.Bounds().Dy() != h {
		t.Fatalf("full size %v", full.Bounds())
	}
	if p := full.Pix[:4]; p[0] != 30 || p[1] != 20 || p[2] != 10 || p[3] != 255 {
		t.Fatalf("channel order %v", p)
	}
	for _, mh := range []int{720, 480, 300, 1079} {
		img := scaleBGRA(raw, w, h, mh, false)
		if img.Bounds().Dy() != mh {
			t.Fatalf("height %d: got %v", mh, img.Bounds())
		}
		if dx := img.Bounds().Dx(); dx < mh*16/9-2 || dx > mh*16/9+2 {
			t.Fatalf("width for %d: %d", mh, dx)
		}
		if p := img.Pix[len(img.Pix)/2 : len(img.Pix)/2+4]; p[0] != 30 || p[1] != 20 || p[2] != 10 {
			t.Fatalf("colour after scale %d: %v", mh, p)
		}
	}
	g := scaleBGRA(raw, w, h, 540, true)
	if p := g.Pix[:3]; p[0] != p[1] || p[1] != p[2] {
		t.Fatalf("gray %v", p)
	}
}

func BenchmarkScale1080to720(b *testing.B) {
	raw := make([]byte, 1920*1080*4)
	for i := 0; i < b.N; i++ {
		scaleBGRA(raw, 1920, 1080, 720, false)
	}
}

func BenchmarkConvert1080(b *testing.B) {
	raw := make([]byte, 1920*1080*4)
	for i := 0; i < b.N; i++ {
		scaleBGRA(raw, 1920, 1080, 0, false)
	}
}
