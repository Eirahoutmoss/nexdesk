// Package tiles holds the wire format of NexDesk's delta screen frames,
// shared by the Windows agent/viewer and the mobile viewer.
//
// A delta frame is a JPEG "atlas" of changed 64x64 tiles. The tile positions
// travel in a JPEG COM segment right after SOI:
//
//	"NXT1" | u16 tileSize | u16 cols | u16 count | count × (u16 tx, u16 ty)
//
// Tile i of the atlas sits at ((i%cols)*size, (i/cols)*size) and belongs at
// (tx*size, ty*size) on the screen. A frame without the segment is a full
// (key) frame.
package tiles

import (
	"bytes"
	"encoding/binary"
	"image"
)

const (
	Size  = 64
	Cols  = 16
	Magic = "NXT1"
)

// Meta describes a delta frame.
type Meta struct {
	Size, Cols int
	Tiles      []image.Point
}

// WithMeta inserts the tile segment into a JPEG.
func WithMeta(jp []byte, cols int, tiles []image.Point) []byte {
	meta := make([]byte, 0, 10+4*len(tiles))
	meta = append(meta, Magic...)
	meta = binary.BigEndian.AppendUint16(meta, Size)
	meta = binary.BigEndian.AppendUint16(meta, uint16(cols))
	meta = binary.BigEndian.AppendUint16(meta, uint16(len(tiles)))
	for _, t := range tiles {
		meta = binary.BigEndian.AppendUint16(meta, uint16(t.X))
		meta = binary.BigEndian.AppendUint16(meta, uint16(t.Y))
	}
	out := make([]byte, 0, len(jp)+len(meta)+4)
	out = append(out, jp[:2]...) // SOI
	out = append(out, 0xFF, 0xFE)
	out = binary.BigEndian.AppendUint16(out, uint16(len(meta)+2))
	out = append(out, meta...)
	return append(out, jp[2:]...)
}

// Parse reads the tile segment; ok=false means a full frame.
func Parse(jp []byte) (Meta, bool) {
	if len(jp) < 14 || jp[2] != 0xFF || jp[3] != 0xFE {
		return Meta{}, false
	}
	l := int(binary.BigEndian.Uint16(jp[4:6]))
	if l < 2+10 || 4+l > len(jp) || string(jp[6:10]) != Magic {
		return Meta{}, false
	}
	m := jp[6 : 4+l]
	tm := Meta{Size: int(binary.BigEndian.Uint16(m[4:6])), Cols: int(binary.BigEndian.Uint16(m[6:8]))}
	n := int(binary.BigEndian.Uint16(m[8:10]))
	if tm.Size <= 0 || tm.Cols <= 0 || len(m) != 10+4*n {
		return Meta{}, false
	}
	for i := 0; i < n; i++ {
		o := 10 + 4*i
		tm.Tiles = append(tm.Tiles, image.Pt(int(binary.BigEndian.Uint16(m[o:])), int(binary.BigEndian.Uint16(m[o+2:]))))
	}
	return tm, true
}

// Changed compares two equally sized frames tile by tile, row by row.
func Changed(a, b *image.RGBA) []image.Point {
	w, h := b.Bounds().Dx(), b.Bounds().Dy()
	tw, th := (w+Size-1)/Size, (h+Size-1)/Size
	var out []image.Point
	for ty := 0; ty < th; ty++ {
		y0, y1 := ty*Size, min((ty+1)*Size, h)
		for tx := 0; tx < tw; tx++ {
			x0, x1 := tx*Size*4, min((tx+1)*Size, w)*4
			for y := y0; y < y1; y++ {
				ra := a.Pix[y*a.Stride+x0 : y*a.Stride+x1]
				rb := b.Pix[y*b.Stride+x0 : y*b.Stride+x1]
				if !bytes.Equal(ra, rb) {
					out = append(out, image.Pt(tx, ty))
					break
				}
			}
		}
	}
	return out
}
