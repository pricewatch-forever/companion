// Command gen draws the Companion's icons, a pocket watch: the menu-bar
// icon (a template image: black, the system colours it) and the app icon.
// Run it with `go generate ./internal/icon`.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

func main() {
	write("menubar.png", draw(36, false))
	write("app.png", draw(1024, true))
}

// shape reports whether a point, in a 1×1 square, is inside it.
type shape func(x, y float64) bool

type layer struct {
	shape shape
	color color.NRGBA
}

func ring(cx, cy, r, w float64) shape {
	return func(x, y float64) bool { return math.Abs(math.Hypot(x-cx, y-cy)-r) <= w/2 }
}

func disc(cx, cy, r float64) shape {
	return func(x, y float64) bool { return math.Hypot(x-cx, y-cy) <= r }
}

// bar is a line from (x1, y1) to (x2, y2), w wide, with round ends.
func bar(x1, y1, x2, y2, w float64) shape {
	return func(x, y float64) bool {
		dx, dy := x2-x1, y2-y1
		t := max(0, min(1, ((x-x1)*dx+(y-y1)*dy)/(dx*dx+dy*dy)))
		return math.Hypot(x-(x1+t*dx), y-(y1+t*dy)) <= w/2
	}
}

func roundedSquare(margin, radius float64) shape {
	return func(x, y float64) bool {
		lo, hi := margin+radius, 1-margin-radius
		dx := max(lo-x, 0, x-hi)
		dy := max(lo-y, 0, y-hi)
		return math.Hypot(dx, dy) <= radius
	}
}

// hand is a watch hand from the centre at an angle in degrees, clockwise
// from 12.
func hand(cx, cy, length, angle, w float64) shape {
	a := angle * math.Pi / 180
	return bar(cx, cy, cx+length*math.Sin(a), cy-length*math.Cos(a), w)
}

func watch(cx, cy, r, stroke float64) []shape {
	return []shape{
		ring(cx, cy, r, stroke),
		bar(cx, cy-r-stroke*0.4, cx, cy-r-stroke*1.6, stroke*1.6), // the crown
		hand(cx, cy, r*0.55, 0, stroke*0.9),
		hand(cx, cy, r*0.42, 120, stroke*0.9),
		disc(cx, cy, stroke*0.8),
	}
}

func draw(size int, app bool) *image.NRGBA {
	var layers []layer
	if app {
		dark := color.NRGBA{0x22, 0x1c, 0x16, 0xff}
		gold := color.NRGBA{0xe8, 0xb6, 0x4c, 0xff}
		face := color.NRGBA{0x35, 0x2b, 0x20, 0xff}
		layers = append(layers, layer{roundedSquare(0.1, 0.18), dark}, layer{disc(0.5, 0.54, 0.27), face})
		for _, s := range watch(0.5, 0.54, 0.27, 0.05) {
			layers = append(layers, layer{s, gold})
		}
	} else {
		for _, s := range watch(0.5, 0.58, 0.3, 0.1) {
			layers = append(layers, layer{s, color.NRGBA{0, 0, 0, 0xff}})
		}
	}
	return render(size, layers)
}

// render draws the layers in order, 4×4 samples to a pixel.
func render(size int, layers []layer) *image.NRGBA {
	const n = 4
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := range size {
		for px := range size {
			var r, g, b, a float64
			for sy := range n {
				for sx := range n {
					x := (float64(px) + (float64(sx)+0.5)/n) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/n) / float64(size)
					var c color.NRGBA
					for _, l := range layers {
						if l.shape(x, y) {
							c = l.color
						}
					}
					ca := float64(c.A) / 255
					r += float64(c.R) * ca
					g += float64(c.G) * ca
					b += float64(c.B) * ca
					a += ca
				}
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / (n * n) * 255)})
			}
		}
	}
	return img
}

func write(name string, img image.Image) {
	f, err := os.Create(name)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
