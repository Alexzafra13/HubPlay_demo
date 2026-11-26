package stream

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// SegmentPlan son los cortes reales de un remux por segmentos: el
// segmento i muestra [Start(i), Start(i+1)) y se genera saltando al
// keyframe Seek(i). Construido a partir del índice de keyframes, así el
// manifest que recibe el reproductor dice exactamente lo que hay en cada
// segmento (ExoPlayer, el de la app de TV, se fía de él para colocar los
// saltos; hls.js lo corrige con los PTS, pero también agradece no tener
// que hacerlo).
type SegmentPlan struct {
	starts []float64 // Lead del keyframe que abre el segmento
	seeks  []float64 // PTS de ese keyframe (destino del -ss)
	end    float64   // fin de la línea temporal
}

// minLastSegment evita un último segmento ridículo (p.ej. 0,2 s de
// créditos): si quedaría por debajo, se funde con el anterior.
const minLastSegment = 1.0

// BuildSegmentPlan agrupa keyframes en segmentos de al menos target
// segundos (el siguiente corte es el primer keyframe cuyo Lead dista
// ≥ target del anterior). duration es la duración conocida del item; si
// es 0 se usa la del índice. Devuelve nil si el índice no sirve.
func BuildSegmentPlan(idx *KeyframeIndex, duration, target float64) *SegmentPlan {
	if idx == nil || len(idx.Points) == 0 || target <= 0 {
		return nil
	}
	end := duration
	if end <= 0 {
		end = idx.MaxPTS
	}
	p := &SegmentPlan{}
	for _, kp := range idx.Points {
		if kp.Lead >= end {
			break
		}
		if n := len(p.starts); n > 0 && kp.Lead < p.starts[n-1]+target-1e-6 {
			continue
		}
		p.starts = append(p.starts, kp.Lead)
		p.seeks = append(p.seeks, kp.PTS)
	}
	if len(p.starts) == 0 {
		return nil
	}
	if n := len(p.starts); n > 1 && end-p.starts[n-1] < minLastSegment {
		p.starts = p.starts[:n-1]
		p.seeks = p.seeks[:n-1]
	}
	p.end = end
	if p.end <= p.starts[len(p.starts)-1] {
		return nil
	}
	return p
}

// Count es el número de segmentos.
func (p *SegmentPlan) Count() int { return len(p.starts) }

// Start es el primer instante de pantalla del segmento i.
func (p *SegmentPlan) Start(i int) float64 { return p.starts[i] }

// End es el final (exclusivo) del segmento i; para el último, el final
// de la línea temporal.
func (p *SegmentPlan) End(i int) float64 {
	if i+1 < len(p.starts) {
		return p.starts[i+1]
	}
	return p.end
}

// IsLast indica si i es el último segmento.
func (p *SegmentPlan) IsLast(i int) bool { return i == len(p.starts)-1 }

// Seek es el PTS del keyframe que abre el segmento i.
func (p *SegmentPlan) Seek(i int) float64 { return p.seeks[i] }

// Duration es la duración declarada del segmento i.
func (p *SegmentPlan) Duration(i int) float64 { return p.End(i) - p.Start(i) }

// SegmentAt devuelve el segmento que contiene el instante t.
func (p *SegmentPlan) SegmentAt(t float64) int {
	i := sort.SearchFloat64s(p.starts, t)
	if i < len(p.starts) && p.starts[i] == t {
		return i
	}
	if i > 0 {
		i--
	}
	return i
}

// Manifest genera el playlist VOD con las duraciones reales.
// segmentURLTemplate lleva un %d (o %05d) para el índice, igual que el
// de SynthesizeVODManifest.
func (p *SegmentPlan) Manifest(segmentURLTemplate string) string {
	maxDur := 0.0
	for i := range p.starts {
		if d := p.Duration(i); d > maxDur {
			maxDur = d
		}
	}
	// RFC 8216 §4.3.3.1: cada EXTINF redondeado al entero más cercano
	// debe ser <= TARGETDURATION.
	target := int(math.Ceil(maxDur))
	if target < 1 {
		target = 1
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", target)
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	for i := range p.starts {
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n", p.Duration(i))
		fmt.Fprintf(&b, segmentURLTemplate+"\n", i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}
