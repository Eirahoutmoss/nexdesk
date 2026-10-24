//go:build windows

package main

// Tile/delta screen encoding. Instead of a full JPEG every frame, the agent
// compares the new capture with the last one it sent in 64x64 tiles and ships
// only the changed tiles, packed into one "atlas" JPEG. The tile positions
// travel in a JPEG COM segment, so the existing frame format, transport and
// validation are unchanged. Unchanged screens send nothing at all.

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eirahoutmoss/remotesupport/shared/tiles"
)

const (
	tileSize     = tiles.Size
	atlasCols    = tiles.Cols
	keyInterval  = 5 * time.Second
	deltaMaxPart = 0.6 // above this share of changed tiles a keyframe is cheaper
)

type tileEncoder struct {
	prev     *image.RGBA
	monitor  int
	lastKey  time.Time
	forceKey atomic.Bool
}

// activeTiles is the encoder of the running capture loop.
var activeTiles atomic.Pointer[tileEncoder]

// requestKeyframeFor makes the next frame of one technician's stream a full
// one; other technicians' streams are not disturbed.
func requestKeyframeFor(p any) {
	if v, ok := tilesByPeer.Load(p); ok {
		v.(*tileEncoder).forceKey.Store(true)
		return
	}
	requestKeyframe()
}

// requestKeyframe makes the next frame a full one (new viewer, lost base).
func requestKeyframe() {
	if e := activeTiles.Load(); e != nil {
		e.forceKey.Store(true)
	}
}

func (e *tileEncoder) reset() { e.prev = nil }

// encode returns the payload to send, or skip=true when nothing changed.
func (e *tileEncoder) encode(img *image.RGBA, monitor, quality int) (payload []byte, skip bool, err error) {
	b := img.Bounds()
	key := e.prev == nil || e.prev.Bounds() != b || e.monitor != monitor ||
		time.Since(e.lastKey) > keyInterval || e.forceKey.Swap(false)
	var dirty []image.Point
	if !key {
		dirty = changedTiles(e.prev, img)
		if len(dirty) == 0 {
			return nil, true, nil
		}
		total := ((b.Dx() + tileSize - 1) / tileSize) * ((b.Dy() + tileSize - 1) / tileSize)
		if float64(len(dirty)) > deltaMaxPart*float64(total) {
			key = true
		}
	}
	var buf bytes.Buffer
	if key {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, false, err
		}
		e.lastKey = time.Now()
		payload = buf.Bytes()
	} else {
		cols := min(len(dirty), atlasCols)
		rows := (len(dirty) + cols - 1) / cols
		atlas := image.NewRGBA(image.Rect(0, 0, cols*tileSize, rows*tileSize))
		for i, t := range dirty {
			src := image.Rect(t.X*tileSize, t.Y*tileSize, (t.X+1)*tileSize, (t.Y+1)*tileSize).Intersect(b)
			dp := image.Pt((i%cols)*tileSize, (i/cols)*tileSize)
			draw.Draw(atlas, image.Rectangle{dp, dp.Add(src.Size())}, img, src.Min, draw.Src)
		}
		if err := jpeg.Encode(&buf, atlas, &jpeg.Options{Quality: quality}); err != nil {
			return nil, false, err
		}
		payload = withTileMeta(buf.Bytes(), cols, dirty)
	}
	e.prev, e.monitor = img, monitor
	return payload, false, nil
}

// The wire format lives in shared/tiles (also used by the mobile viewer).
func changedTiles(a, b *image.RGBA) []image.Point { return tiles.Changed(a, b) }

func withTileMeta(jp []byte, cols int, t []image.Point) []byte { return tiles.WithMeta(jp, cols, t) }

type tileMeta struct {
	size, cols int
	tiles      []image.Point
}

func parseTileMeta(jp []byte) (tileMeta, bool) {
	m, ok := tiles.Parse(jp)
	return tileMeta{size: m.Size, cols: m.Cols, tiles: m.Tiles}, ok
}

var keyReqMu sync.Mutex
var keyReqAt time.Time

// applyTileDelta patches the viewer's BGRA buffer with the tiles of a delta
// frame. Returns false (and asks for a keyframe) when there is no matching base.
func applyTileDelta(width, height int, jp []byte, tm tileMeta) bool {
	img, err := jpeg.Decode(bytes.NewReader(jp))
	if err != nil {
		return false
	}
	ab := img.Bounds()
	atlas := image.NewRGBA(image.Rect(0, 0, ab.Dx(), ab.Dy()))
	draw.Draw(atlas, atlas.Bounds(), img, ab.Min, draw.Src)
	viewer.mu.Lock()
	ok := viewer.pixels != nil && viewer.width == width && viewer.height == height
	if ok {
		px := viewer.pixels
		for i, t := range tm.tiles {
			ax, ay := (i%tm.cols)*tm.size, (i/tm.cols)*tm.size
			x0, y0 := t.X*tm.size, t.Y*tm.size
			w, h := min(tm.size, width-x0), min(tm.size, height-y0)
			if w <= 0 || h <= 0 || ax+w > atlas.Rect.Dx() || ay+h > atlas.Rect.Dy() {
				continue
			}
			for y := 0; y < h; y++ {
				s := atlas.Pix[(ay+y)*atlas.Stride+ax*4:]
				d := px[((y0+y)*width+x0)*4:]
				for x := 0; x < w; x++ {
					d[x*4], d[x*4+1], d[x*4+2] = s[x*4+2], s[x*4+1], s[x*4] // RGBA -> BGRA
				}
			}
		}
	}
	h := viewer.hwnd
	viewer.mu.Unlock()
	if !ok {
		keyReqMu.Lock()
		if time.Since(keyReqAt) > time.Second {
			keyReqAt = time.Now()
			sendCtl("KEYFRAME")
		}
		keyReqMu.Unlock()
		return false
	}
	framesRecv.Add(1)
	if h != 0 {
		invalidateRect.Call(h, 0, 0)
	}
	return true
}
