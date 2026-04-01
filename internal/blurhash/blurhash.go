package blurhash

import (
	"image"
	"image/color"
	"math"
	"strings"
)

const base83Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

// Encode computes a blurhash string from the given image.
// xComponents and yComponents control the level of detail (typical: 4, 3).
func Encode(xComponents, yComponents int, img image.Image) string {
	// Downsample to a small image for speed.
	small := downsample(img, 32, 32)
	w := small.Bounds().Dx()
	h := small.Bounds().Dy()

	// Extract linear RGB values.
	pixels := make([][3]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := small.At(x, y).RGBA()
			pixels[y*w+x] = [3]float64{
				sRGBToLinear(float64(r) / 65535.0),
				sRGBToLinear(float64(g) / 65535.0),
				sRGBToLinear(float64(b) / 65535.0),
			}
		}
	}

	// Compute DCT components.
	components := make([][3]float64, xComponents*yComponents)
	for j := 0; j < yComponents; j++ {
		for i := 0; i < xComponents; i++ {
			var r, g, b float64
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					basis := math.Cos(math.Pi*float64(i)*float64(x)/float64(w)) *
						math.Cos(math.Pi*float64(j)*float64(y)/float64(h))
					px := pixels[y*w+x]
					r += basis * px[0]
					g += basis * px[1]
					b += basis * px[2]
				}
			}
			scale := 1.0 / float64(w*h)
			if i != 0 || j != 0 {
				scale = 2.0 / float64(w*h)
			}
			components[j*xComponents+i] = [3]float64{r * scale, g * scale, b * scale}
		}
	}

	// Encode to base83.
	var buf strings.Builder

	// Size flag: (xComponents - 1) + (yComponents - 1) * 9
	sizeFlag := (xComponents - 1) + (yComponents-1)*9
	buf.WriteString(encodeBase83(sizeFlag, 1))

	// Quantised maximum AC value.
	var maximumValue float64
	if len(components) > 1 {
		for _, c := range components[1:] {
			for _, v := range c {
				if math.Abs(v) > maximumValue {
					maximumValue = math.Abs(v)
				}
			}
		}
	}
	quantisedMaximumValue := 0
	if maximumValue > 0 {
		quantisedMaximumValue = clampInt(int(math.Floor(maximumValue*166-0.5)), 0, 82)
	}
	buf.WriteString(encodeBase83(quantisedMaximumValue, 1))

	realMaximumValue := (float64(quantisedMaximumValue) + 1) / 167.0

	// DC value.
	dc := components[0]
	buf.WriteString(encodeBase83(encodeDC(dc), 4))

	// AC values.
	for _, ac := range components[1:] {
		buf.WriteString(encodeBase83(encodeAC(ac, realMaximumValue), 2))
	}

	return buf.String()
}

func sRGBToLinear(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func linearToSRGB(value float64) float64 {
	if value <= 0.0031308 {
		return value * 12.92
	}
	return 1.055*math.Pow(value, 1.0/2.4) - 0.055
}

func encodeDC(c [3]float64) int {
	r := linearToSRGB(c[0])
	g := linearToSRGB(c[1])
	b := linearToSRGB(c[2])
	return (clampInt(int(r*255+0.5), 0, 255) << 16) +
		(clampInt(int(g*255+0.5), 0, 255) << 8) +
		clampInt(int(b*255+0.5), 0, 255)
}

func encodeAC(c [3]float64, maximumValue float64) int {
	quant := func(v float64) int {
		return clampInt(int(math.Floor(signPow(v/maximumValue, 0.5)*9+9.5)), 0, 18)
	}
	return quant(c[0])*19*19 + quant(c[1])*19 + quant(c[2])
}

func signPow(value, exp float64) float64 {
	if value < 0 {
		return -math.Pow(-value, exp)
	}
	return math.Pow(value, exp)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func encodeBase83(value, length int) string {
	result := make([]byte, length)
	for i := 1; i <= length; i++ {
		digit := (value / pow83(length-i)) % 83
		result[i-1] = base83Chars[digit]
	}
	return string(result)
}

func pow83(exp int) int {
	result := 1
	for i := 0; i < exp; i++ {
		result *= 83
	}
	return result
}

// downsample resizes an image to the given dimensions using nearest-neighbor.
func downsample(src image.Image, dstW, dstH int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := bounds.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := bounds.Min.X + x*srcW/dstW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

// EncodeFromRGBA is a convenience wrapper that accepts an *image.RGBA.
func EncodeFromRGBA(img *image.RGBA) string {
	return Encode(4, 3, img)
}

// GenerateTestImage creates a simple solid-color image for testing.
func GenerateTestImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}
