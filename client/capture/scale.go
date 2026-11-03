package capture

import "image"

// scaleBGRA turns a top-down BGRA screen buffer into an RGBA image, scaled
// down to maxHeight (0 = keep the size) in the same pass. Large reductions
// first halve the image with 2x2 averaging, so text stays readable instead
// of losing whole pixel rows; the rest is bilinear.
func scaleBGRA(raw []byte, w, h, maxHeight int, gray bool) *image.RGBA {
	if maxHeight <= 0 || h <= maxHeight {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		convertRows(img.Pix, img.Stride, raw, w*4, w, h, gray)
		return img
	}
	src, sw, sh := raw, w, h
	for sh/2 >= maxHeight && sw >= 2 {
		src, sw, sh = halveBGRA(src, sw, sh)
	}
	dstH := maxHeight
	dstW := max(1, sw*dstH/sh)
	if dstH == sh {
		img := image.NewRGBA(image.Rect(0, 0, sw, sh))
		convertRows(img.Pix, img.Stride, src, sw*4, sw, sh, gray)
		return img
	}
	return bilinearBGRA(src, sw, sh, dstW, dstH, gray)
}

func convertRows(dst []byte, dstStride int, src []byte, srcStride, w, h int, gray bool) {
	for y := 0; y < h; y++ {
		s := src[y*srcStride : y*srcStride+w*4]
		d := dst[y*dstStride : y*dstStride+w*4]
		for i := 0; i < len(s); i += 4 {
			b, g, r := s[i], s[i+1], s[i+2]
			if gray {
				l := uint8((uint32(r)*77 + uint32(g)*150 + uint32(b)*29) >> 8)
				r, g, b = l, l, l
			}
			d[i], d[i+1], d[i+2], d[i+3] = r, g, b, 0xff
		}
	}
}

// halveBGRA averages 2x2 blocks (BGRA in, BGRA out).
func halveBGRA(src []byte, w, h int) ([]byte, int, int) {
	nw, nh := w/2, h/2
	out := make([]byte, nw*nh*4)
	for y := 0; y < nh; y++ {
		r0 := src[(2*y)*w*4:]
		r1 := src[(2*y+1)*w*4:]
		d := out[y*nw*4:]
		for x := 0; x < nw; x++ {
			i := x * 8
			for c := 0; c < 3; c++ {
				d[x*4+c] = uint8((uint32(r0[i+c]) + uint32(r0[i+4+c]) + uint32(r1[i+c]) + uint32(r1[i+4+c]) + 2) >> 2)
			}
		}
	}
	return out, nw, nh
}

// bilinearBGRA scales BGRA src to an RGBA image of dstW x dstH.
func bilinearBGRA(src []byte, srcW, srcH, dstW, dstH int, gray bool) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	x0s := make([]int, dstW)
	x1s := make([]int, dstW)
	fxs := make([]uint32, dstW)
	for x := 0; x < dstW; x++ {
		sx := (float64(x)+0.5)*float64(srcW)/float64(dstW) - 0.5
		x0 := int(sx)
		if sx < 0 {
			x0, sx = 0, 0
		}
		fx := sx - float64(x0)
		x1 := min(x0+1, srcW-1)
		x0s[x], x1s[x], fxs[x] = x0*4, x1*4, uint32(fx*256)
	}
	ss := srcW * 4
	for y := 0; y < dstH; y++ {
		sy := (float64(y)+0.5)*float64(srcH)/float64(dstH) - 0.5
		y0 := int(sy)
		if sy < 0 {
			y0, sy = 0, 0
		}
		wy := uint32((sy - float64(y0)) * 256)
		y1 := min(y0+1, srcH-1)
		r0, r1 := src[y0*ss:], src[y1*ss:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < dstW; x++ {
			a, b, wx := x0s[x], x1s[x], fxs[x]
			var px [3]uint8
			for c := 0; c < 3; c++ {
				top := uint32(r0[a+c])*(256-wx) + uint32(r0[b+c])*wx
				bot := uint32(r1[a+c])*(256-wx) + uint32(r1[b+c])*wx
				px[c] = uint8((top*(256-wy) + bot*wy + 32768) >> 16)
			}
			bl, g, r := px[0], px[1], px[2]
			if gray {
				l := uint8((uint32(r)*77 + uint32(g)*150 + uint32(bl)*29) >> 8)
				r, g, bl = l, l, l
			}
			d[x*4], d[x*4+1], d[x*4+2], d[x*4+3] = r, g, bl, 0xff
		}
	}
	return dst
}
