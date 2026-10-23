package main

// Images for `athanor screenshots`, computed as Pillow computes them (athanor.py used it): a PNG read as
// Pillow's "RGB", resized with its bicubic filter (fixed point, 22 bits), and its statistics, so that the
// differences and the "almost uniform" verdicts are the same to the last digit printed.

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

// imageRGB: 3 bytes per pixel, rows one after the other.
type imageRGB struct {
	l, h int
	pix  []byte
}

func (im *imageRGB) at(x, y int) []byte { i := (y*im.l + x) * 3; return im.pix[i : i+3] }

// lireRGB: a PNG as Pillow's Image.open(...).convert("RGB"): the alpha is dropped (not blended), 16-bit
// colours keep their high byte, 16-bit grey is clipped to 255, a palette gives its colours.
func lireRGB(chemin string) (*imageRGB, error) {
	f, err := os.Open(chemin)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return versRGB(src), nil
}

func versRGB(src image.Image) *imageRGB {
	b := src.Bounds()
	im := &imageRGB{l: b.Dx(), h: b.Dy(), pix: make([]byte, b.Dx()*b.Dy()*3)}
	i := 0
	poser := func(r, g, bl uint8) { im.pix[i], im.pix[i+1], im.pix[i+2] = r, g, bl; i += 3 }
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			switch s := src.(type) {
			case *image.NRGBA:
				p := s.Pix[s.PixOffset(x, y):]
				poser(p[0], p[1], p[2])
			case *image.RGBA: // opaque in what png gives
				p := s.Pix[s.PixOffset(x, y):]
				poser(p[0], p[1], p[2])
			case *image.NRGBA64:
				p := s.Pix[s.PixOffset(x, y):]
				poser(p[0], p[2], p[4])
			case *image.RGBA64:
				p := s.Pix[s.PixOffset(x, y):]
				poser(p[0], p[2], p[4])
			case *image.Gray:
				v := s.Pix[s.PixOffset(x, y)]
				poser(v, v, v)
			case *image.Gray16:
				p := s.Pix[s.PixOffset(x, y):]
				v := int(p[0])<<8 | int(p[1])
				if v > 255 {
					v = 255
				}
				poser(uint8(v), uint8(v), uint8(v))
			case *image.Paletted:
				c := color.NRGBAModel.Convert(s.Palette[s.ColorIndexAt(x, y)]).(color.NRGBA)
				if a, ok := s.Palette[s.ColorIndexAt(x, y)].(color.RGBA); ok && a.A == 255 {
					c = color.NRGBA{a.R, a.G, a.B, 255}
				}
				poser(c.R, c.G, c.B)
			default:
				c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
				poser(c.R, c.G, c.B)
			}
		}
	}
	return im
}

// filtreBicubique: Pillow's bicubic (a = -0.5), support 2.
func filtreBicubique(x float64) float64 {
	const a = -0.5
	x = math.Abs(x)
	if x < 1 {
		return ((a+2)*x-(a+3))*x*x + 1
	}
	if x < 2 {
		return (((x-5)*x+8)*x - 4) * a
	}
	return 0
}

const bitsPrecision = 32 - 8 - 2

// coefficients: Pillow's precompute_coeffs then normalize_coeffs_8bpc, for inSize -> outSize.
func coefficients(inSize, outSize int) (ksize int, bornes []int, kk []int32) {
	scale := float64(inSize) / float64(outSize)
	fs := scale
	if fs < 1 {
		fs = 1
	}
	support := 2 * fs
	ksize = int(math.Ceil(support))*2 + 1
	pre := make([]float64, outSize*ksize)
	bornes = make([]int, outSize*2)
	for xx := 0; xx < outSize; xx++ {
		centre := (float64(xx) + 0.5) * scale
		ww, ss := 0.0, 1.0/fs
		xmin := int(centre - support + 0.5)
		if xmin < 0 {
			xmin = 0
		}
		xmax := int(centre + support + 0.5)
		if xmax > inSize {
			xmax = inSize
		}
		xmax -= xmin
		k := pre[xx*ksize:]
		for x := 0; x < xmax; x++ {
			w := filtreBicubique((float64(x+xmin) - centre + 0.5) * ss)
			k[x] = w
			ww += w
		}
		for x := 0; x < xmax; x++ {
			if ww != 0 {
				k[x] /= ww
			}
		}
		bornes[xx*2], bornes[xx*2+1] = xmin, xmax
	}
	kk = make([]int32, len(pre))
	for i, v := range pre {
		if v < 0 {
			kk[i] = int32(-0.5 + v*(1<<bitsPrecision))
		} else {
			kk[i] = int32(0.5 + v*(1<<bitsPrecision))
		}
	}
	return
}

func clip8(v int32) byte {
	v >>= bitsPrecision
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// redimensionner: Image.resize((l, h)) with Pillow's default (bicubic) for an RGB image.
func redimensionner(im *imageRGB, l, h int) *imageRGB {
	if im.l == l && im.h == h {
		return &imageRGB{l: l, h: h, pix: append([]byte(nil), im.pix...)}
	}
	kh, bh, kkh := coefficients(im.l, l)
	kv, bv, kkv := coefficients(im.h, h)
	src := im
	if l != im.l {
		premiere := bv[0]
		derniere := bv[h*2-2] + bv[h*2-1]
		for i := 0; i < h; i++ {
			bv[i*2] -= premiere
		}
		tmp := &imageRGB{l: l, h: derniere - premiere, pix: make([]byte, l*(derniere-premiere)*3)}
		for yy := 0; yy < tmp.h; yy++ {
			for xx := 0; xx < l; xx++ {
				xmin, xmax := bh[xx*2], bh[xx*2+1]
				k := kkh[xx*kh:]
				s := [3]int32{1 << (bitsPrecision - 1), 1 << (bitsPrecision - 1), 1 << (bitsPrecision - 1)}
				for x := 0; x < xmax; x++ {
					p := src.at(x+xmin, yy+premiere)
					s[0] += int32(p[0]) * k[x]
					s[1] += int32(p[1]) * k[x]
					s[2] += int32(p[2]) * k[x]
				}
				o := tmp.at(xx, yy)
				o[0], o[1], o[2] = clip8(s[0]), clip8(s[1]), clip8(s[2])
			}
		}
		src = tmp
	}
	if h == im.h {
		return src
	}
	out := &imageRGB{l: l, h: h, pix: make([]byte, l*h*3)}
	for yy := 0; yy < h; yy++ {
		ymin, ymax := bv[yy*2], bv[yy*2+1]
		k := kkv[yy*kv:]
		for xx := 0; xx < l; xx++ {
			s := [3]int32{1 << (bitsPrecision - 1), 1 << (bitsPrecision - 1), 1 << (bitsPrecision - 1)}
			for y := 0; y < ymax; y++ {
				p := src.at(xx, y+ymin)
				s[0] += int32(p[0]) * k[y]
				s[1] += int32(p[1]) * k[y]
				s[2] += int32(p[2]) * k[y]
			}
			o := out.at(xx, yy)
			o[0], o[1], o[2] = clip8(s[0]), clip8(s[1]), clip8(s[2])
		}
	}
	return out
}

// statistiques: ImageStat.Stat(im).mean and .stddev of each band.
func statistiques(im *imageRGB) (moyenne, ecart [3]float64) {
	var h [3][256]int
	for i := 0; i < len(im.pix); i += 3 {
		h[0][im.pix[i]]++
		h[1][im.pix[i+1]]++
		h[2][im.pix[i+2]]++
	}
	for b := 0; b < 3; b++ {
		n, s, s2 := 0, 0.0, 0.0
		for j := 0; j < 256; j++ {
			n += h[b][j]
			s += float64(j * h[b][j])
			s2 += float64(j*j) * float64(h[b][j])
		}
		if n == 0 {
			continue
		}
		moyenne[b] = s / float64(n)
		ecart[b] = math.Sqrt((s2 - math.Pow(s, 2)/float64(n)) / float64(n))
	}
	return
}

// difference: ImageChops.difference(a, b) (same size).
func difference(a, b *imageRGB) *imageRGB {
	d := &imageRGB{l: a.l, h: a.h, pix: make([]byte, len(a.pix))}
	for i := range a.pix {
		x, y := int(a.pix[i]), int(b.pix[i])
		if x > y {
			d.pix[i] = byte(x - y)
		} else {
			d.pix[i] = byte(y - x)
		}
	}
	return d
}
