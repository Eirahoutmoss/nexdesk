package tiles

import (
	"image"
	"testing"
)

func TestMetaRoundTrip(t *testing.T) {
	jp := []byte{0xFF, 0xD8, 0xFF, 0xDB, 1, 2, 3}
	want := []image.Point{{1, 2}, {15, 0}, {3, 40}}
	out := WithMeta(jp, Cols, want)
	m, ok := Parse(out)
	if !ok || m.Size != Size || m.Cols != Cols || len(m.Tiles) != 3 || m.Tiles[2] != want[2] {
		t.Fatalf("%+v %v", m, ok)
	}
	if _, ok := Parse(jp); ok {
		t.Fatal("plain JPEG parsed as delta")
	}
}

func TestChanged(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 130, 70))
	b := image.NewRGBA(image.Rect(0, 0, 130, 70))
	b.Pix[(65*b.Stride)+129*4] = 9 // tile (2,1)
	got := Changed(a, b)
	if len(got) != 1 || got[0] != image.Pt(2, 1) {
		t.Fatalf("%v", got)
	}
}
