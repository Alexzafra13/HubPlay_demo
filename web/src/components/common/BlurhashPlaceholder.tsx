import { useEffect, useRef } from "react";
import { decode, isBlurhashValid } from "blurhash";

// Canvas-based placeholder that decodes a BlurHash string and paints it
// at a tiny resolution (32×32 by default). The browser then upscales
// the canvas via CSS to fill the parent — that's the whole point of
// BlurHash, the upscale IS the blur. Decoding 32×32 stays under 1 ms
// even on low-end hardware so this can render eagerly above the fold.
//
// Why not the libvibrant runtime fallback path or the precomputed
// dominant_color we already paint as backgroundColor: those are
// flat-colour placeholders. BlurHash gives a low-frequency preview of
// the actual poster — readable enough that the user sees "this is the
// Stranger Things poster" before the real <img> decodes, instead of
// "this is dark teal". Compounded across a 30-card grid the perceived
// LCP improvement is significant.
//
// The wire field is `poster_blurhash` (canonical name across the codebase
// since the migration that added the column). When absent, callers
// should not render this component at all — silent fallback to the
// existing dominant_color flat tint is the right behaviour.

export interface BlurhashPlaceholderProps {
  hash: string;
  // Resolution to decode at. 32×32 is the sweet spot: small enough to
  // decode in <1 ms, big enough that CSS upscaling keeps the gradient
  // smooth instead of pixelating. Callers that need a different aspect
  // (e.g. backdrops are 16:9) pass dimensions explicitly.
  width?: number;
  height?: number;
  // Optional className — typically Tailwind sizing + opacity transition
  // applied by the parent so the placeholder fades out as the real
  // image loads. The component itself only sets dimensions inline.
  className?: string;
  // punch defaults to 1 (the value the encoder used). Some references
  // recommend 1.0–1.2 for posters; raising it makes colour pop but can
  // wash out detail. Stick with 1 unless we have a reason.
  punch?: number;
}

export function BlurhashPlaceholder({
  hash,
  width = 32,
  height = 32,
  className,
  punch = 1,
}: BlurhashPlaceholderProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    // Validate before decode — `decode` throws on a malformed hash, and
    // an old row in the DB or a corrupted column read could surface
    // nonsense. Silent no-op is correct: the parent already paints a
    // dominant_color tint underneath.
    if (!isBlurhashValid(hash).result) return;

    const pixels = decode(hash, width, height, punch);
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const imageData = ctx.createImageData(width, height);
    imageData.data.set(pixels);
    ctx.putImageData(imageData, 0, 0);
  }, [hash, width, height, punch]);

  return (
    <canvas
      ref={canvasRef}
      width={width}
      height={height}
      aria-hidden="true"
      className={className}
    />
  );
}
