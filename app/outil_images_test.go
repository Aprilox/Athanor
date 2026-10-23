package main

// The images of `athanor screenshots` against Pillow: the same pixels after the resize, the same statistics.

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func pillow(t *testing.T) {
	t.Helper()
	if exec.Command("python3", "-c", "import PIL").Run() != nil {
		t.Skip("no Pillow")
	}
}

// imagesDeTest: PNGs of every kind png can hold, of sizes around 160x90 and far from it.
func imagesDeTest(t *testing.T, d string) []string {
	r := rand.New(rand.NewSource(7))
	var fichiers []string
	ecrire := func(nom string, im image.Image) {
		var b bytes.Buffer
		if err := png.Encode(&b, im); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(d, nom)
		_ = os.WriteFile(f, b.Bytes(), 0o644)
		fichiers = append(fichiers, f)
	}
	tailles := [][2]int{{160, 90}, {1920, 1080}, {1280, 720}, {161, 91}, {80, 45}, {3, 2}, {1, 1}, {333, 777}, {1366, 768}, {2560, 1440}, {159, 300}}
	for i, tl := range tailles {
		l, h := tl[0], tl[1]
		nrgba := image.NewNRGBA(image.Rect(0, 0, l, h))
		lisse := image.NewRGBA(image.Rect(0, 0, l, h))
		gris := image.NewGray(image.Rect(0, 0, l, h))
		for y := 0; y < h; y++ {
			for x := 0; x < l; x++ {
				nrgba.Set(x, y, color.NRGBA{uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256)), uint8(r.Intn(256))})
				lisse.Set(x, y, color.RGBA{uint8(x * 255 / max(l, 1)), uint8(y * 255 / max(h, 1)), uint8((x + y) % 256), 255})
				gris.Set(x, y, color.Gray{uint8((x*7 + y*3) % 256)})
			}
		}
		ecrire(fmt.Sprintf("nrgba-%d.png", i), nrgba)
		ecrire(fmt.Sprintf("lisse-%d.png", i), lisse)
		ecrire(fmt.Sprintf("gris-%d.png", i), gris)
	}
	pal := image.NewPaletted(image.Rect(0, 0, 640, 360), color.Palette{color.RGBA{10, 20, 30, 255}, color.NRGBA{200, 100, 50, 128}, color.RGBA{255, 255, 255, 255}})
	g16 := image.NewGray16(image.Rect(0, 0, 300, 200))
	c16 := image.NewNRGBA64(image.Rect(0, 0, 300, 200))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			pal.SetColorIndex(x, y, uint8((x/10+y/10)%3))
			if x < 300 && y < 200 {
				g16.Set(x, y, color.Gray16{uint16(r.Intn(400))})
				c16.Set(x, y, color.NRGBA64{uint16(r.Intn(65536)), uint16(r.Intn(65536)), uint16(r.Intn(65536)), 65535})
			}
		}
	}
	ecrire("palette.png", pal)
	ecrire("gris16.png", g16)
	ecrire("couleur16.png", c16)
	return fichiers
}

func TestImagesPillow(t *testing.T) {
	pillow(t)
	d := t.TempDir()
	fichiers := imagesDeTest(t, d)
	script := `import sys, hashlib
from PIL import Image, ImageStat
for f in sys.argv[1:]:
    im = Image.open(f).convert("RGB")
    p = im.resize((160, 90))
    s = ImageStat.Stat(p)
    print(im.size[0], im.size[1], hashlib.sha1(im.tobytes()).hexdigest(), hashlib.sha1(p.tobytes()).hexdigest(), repr(s.mean), repr(s.stddev))
`
	out, err := exec.Command("python3", append([]string{"-I", "-c", script}, fichiers...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	lignes := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, f := range fichiers {
		im, err := lireRGB(f)
		if err != nil {
			t.Fatal(err)
		}
		p := redimensionner(im, 160, 90)
		m, e := statistiques(p)
		g := fmt.Sprintf("%d %d %s %s %s %s", im.l, im.h, sha1Hex(im.pix), sha1Hex(p.pix), listePython(m[:]), listePython(e[:]))
		if g != lignes[i] {
			t.Errorf("%s:\n go %s\n py %s", filepath.Base(f), g, lignes[i])
		}
	}
}

func listePython(v []float64) string {
	var s []string
	for _, x := range v {
		s = append(s, reprFlottant(x))
	}
	return "[" + strings.Join(s, ", ") + "]"
}

func sha1Hex(b []byte) string { return fmt.Sprintf("%x", sha1.Sum(b)) }
