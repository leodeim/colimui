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

// A rounded C monogram in a 256-unit design space. Its open center and broad
// terminals stay legible when macOS reduces it to menu bar size.
const unit = float64(size) / 256

var monogram = []point{
	{198, 45}, {100, 45}, {78, 51}, {61, 66}, {49, 88}, {45, 106},
	{45, 150}, {49, 168}, {61, 190}, {78, 205}, {100, 211}, {198, 211},
}

const strokeRadius = 11

func alphaAt(x, y float64) uint8 {
	p := point{x / unit, y / unit}
	distance := math.Inf(1)
	for i := 1; i < len(monogram); i++ {
		distance = math.Min(distance, segment(p, monogram[i-1], monogram[i]))
	}
	distance -= strokeRadius
	// Supersampling handles most edge smoothing; this narrow band softens steps.
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
