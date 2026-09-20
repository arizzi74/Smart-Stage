//go:build ignore

// Development-only analysis of captured native display pixels.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"os"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	bounds := im.Bounds()
	var count, black, opaque, red, green, blue int
	hash := sha256.New()
	for y := bounds.Min.Y + 4; y < bounds.Max.Y; y += 8 {
		for x := bounds.Min.X + 4; x < bounds.Max.X; x += 8 {
			r, g, b, a := im.At(x, y).RGBA()
			count++
			if r <= 2048 && g <= 2048 && b <= 2048 {
				black++
			}
			if a == 65535 {
				opaque++
			}
			if r > 30000 && r > 2*g && r > 2*b {
				red++
			}
			if g > 30000 && g > 2*r && g > 2*b {
				green++
			}
			if b > 30000 && b > 2*r && b > 2*g {
				blue++
			}
			hash.Write([]byte{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(a >> 8)})
		}
	}
	if count == 0 {
		panic("empty captured display")
	}
	fraction := func(n int) float64 { return float64(n) / float64(count) }
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"width": bounds.Dx(), "height": bounds.Dy(), "sampledPixels": count,
		"blackFraction": fraction(black), "opaqueFraction": fraction(opaque),
		"redFraction": fraction(red), "greenFraction": fraction(green), "blueFraction": fraction(blue),
		"sampledPixelSHA256": hex.EncodeToString(hash.Sum(nil)),
	}); err != nil {
		panic(err)
	}
}
