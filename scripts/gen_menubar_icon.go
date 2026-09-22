//go:build ignore

// Generates assets/menubar-template.png, the macOS menu bar template icon.
//
// Run from the repository root: go run scripts/gen_menubar_icon.go
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const (
	outSize = 64
	scale   = 8
	size    = outSize * scale
)

func main() {
	big := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			big.SetNRGBA(x, y, color.NRGBA{A: alphaAt(float64(x)+0.5, float64(y)+0.5)})
		}
	}

	out := image.NewNRGBA(image.Rect(0, 0, outSize, outSize))
	for y := range outSize {
		for x := range outSize {
			var sum uint64
			for sy := range scale {
				for sx := range scale {
					sum += uint64(big.NRGBAAt(x*scale+sx, y*scale+sy).A)
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{A: uint8(sum / (scale * scale))})
		}
	}

	file, err := os.Create("assets/menubar-template.png")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, out); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote assets/menubar-template.png (%dx%d)", outSize, outSize)
}

// Geometry in a 256-unit design space, scaled to the render size.
const unit = float64(size) / 256

var mountains = []point{{6, 248}, {94, 14}, {135, 122}, {189, 71}, {250, 248}}

func alphaAt(x, y float64) uint8 {
	p := point{x / unit, y / unit}
	shape := polygon(p, mountains) - 5
	prompt := math.Min(
		math.Min(
			segment(p, point{78, 137}, point{117, 171}),
			segment(p, point{117, 174}, point{78, 208}),
		),
		segment(p, point{136, 210}, point{188, 210}),
	) - 16
	distance := math.Max(shape, -prompt)
	// 0.7px anti-aliasing band in design units
	switch {
	case distance <= -0.35:
		return 255
	case distance >= 0.35:
		return 0
	default:
		return uint8(255 * (0.5 - distance/0.7))
	}
}

type point struct{ x, y float64 }

// polygon is the signed distance to a simple polygon (negative inside).
func polygon(p point, v []point) float64 {
	d := math.Inf(1)
	sign := 1.0
	for i, j := 0, len(v)-1; i < len(v); j, i = i, i+1 {
		d = math.Min(d, segment(p, v[j], v[i]))
		ex, ey := v[i].x-v[j].x, v[i].y-v[j].y
		wx, wy := p.x-v[j].x, p.y-v[j].y
		c1, c2, c3 := p.y >= v[j].y, p.y < v[i].y, ex*wy > ey*wx
		if (c1 && c2 && c3) || (!c1 && !c2 && !c3) {
			sign = -sign
		}
	}
	return sign * d
}

// segment is the distance to the line segment ab.
func segment(p, a, b point) float64 {
	abx, aby := b.x-a.x, b.y-a.y
	apx, apy := p.x-a.x, p.y-a.y
	t := 0.0
	if lengthSq := abx*abx + aby*aby; lengthSq > 0 {
		t = math.Max(0, math.Min(1, (apx*abx+apy*aby)/lengthSq))
	}
	return math.Hypot(apx-t*abx, apy-t*aby)
}
