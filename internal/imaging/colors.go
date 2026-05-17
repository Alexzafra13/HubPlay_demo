package imaging

import (
	"bytes"
	"fmt"
	"image"
	"log/slog"
	"math"
)

// ExtractDominantColors devuelve dos colores CSS — uno vibrante y otro
// apagado oscuro — que el frontend usa para pintar el degradado del hero
// de cada serie/película sin tener que procesar la imagen en el navegador.
//
// El algoritmo es muy ligero a propósito: muestrea unos pocos píxeles a lo
// largo de la imagen, los agrupa por color parecido, y elige dos ganadores:
//   - el vibrante: el color más saturado dentro de un rango de luminosidad
//     intermedia, para evitar zonas demasiado claras o demasiado oscuras.
//   - el apagado oscuro: el color con tono más neutro entre los oscuros,
//     para que se vea bien junto al fondo gris de la página.
//
// Devuelve cadenas vacías si la imagen no se puede decodificar; en ese
// caso el frontend extrae los colores al vuelo.
func ExtractDominantColors(data []byte, logger *slog.Logger) (vibrant, muted string) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if logger != nil {
			logger.Warn("failed to decode image for dominant colors", "error", err)
		}
		return "", ""
	}
	return paletteFromImage(img)
}

func paletteFromImage(img image.Image) (vibrant, muted string) {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return "", ""
	}

	// Muestreamos unos 32 pasos a lo largo del eje más grande. Así el
	// coste se mantiene constante (~1024 píxeles leídos) por mucho que
	// crezca la imagen original.
	long := w
	if h > long {
		long = h
	}
	step := long / 32
	if step < 1 {
		step = 1
	}

	type bucket struct {
		rSum, gSum, bSum int
		count            int
	}
	buckets := make(map[uint16]*bucket, 256)

	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0x8000 {
				continue // píxel casi transparente, no nos sirve
			}
			r := int(r16 >> 8)
			g := int(g16 >> 8)
			b := int(b16 >> 8)

			key := uint16((r>>4)&0xF)<<8 | uint16((g>>4)&0xF)<<4 | uint16((b>>4)&0xF)
			bk, ok := buckets[key]
			if !ok {
				bk = &bucket{}
				buckets[key] = bk
			}
			bk.rSum += r
			bk.gSum += g
			bk.bSum += b
			bk.count++
		}
	}

	var (
		bestVibrant       [3]int
		bestMuted         [3]int
		vibrantScore      float64
		mutedScore        float64
	)

	for _, bk := range buckets {
		if bk.count == 0 {
			continue
		}
		r := float64(bk.rSum) / float64(bk.count) / 255.0
		g := float64(bk.gSum) / float64(bk.count) / 255.0
		b := float64(bk.bSum) / float64(bk.count) / 255.0

		maxC := math.Max(math.Max(r, g), b)
		minC := math.Min(math.Min(r, g), b)
		l := (maxC + minC) / 2
		var s float64
		if maxC != minC {
			d := maxC - minC
			if l < 0.5 {
				s = d / (maxC + minC)
			} else {
				s = d / (2 - maxC - minC)
			}
		}

		if l >= 0.20 && l <= 0.80 {
			score := s * float64(bk.count)
			if score > vibrantScore {
				vibrantScore = score
				bestVibrant = [3]int{int(r * 255), int(g * 255), int(b * 255)}
			}
		}

		if l <= 0.40 {
			score := (1 - s*0.5) * float64(bk.count)
			if score > mutedScore {
				mutedScore = score
				bestMuted = [3]int{int(r * 255), int(g * 255), int(b * 255)}
			}
		}
	}

	if vibrantScore > 0 {
		vibrant = fmt.Sprintf("rgb(%d, %d, %d)", bestVibrant[0], bestVibrant[1], bestVibrant[2])
	}
	if mutedScore > 0 {
		muted = fmt.Sprintf("rgb(%d, %d, %d)", bestMuted[0], bestMuted[1], bestMuted[2])
	}
	return vibrant, muted
}
