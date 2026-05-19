// extractCode — helper puro reusado por QRScannerModal. Vive en su
// propio archivo (no en QRScannerModal.tsx) porque mezclar componentes
// con utilidades en el mismo módulo rompe React Refresh (HMR pierde
// estado al editar). La regla `react-refresh/only-export-components`
// gateaa esto.

// extractCode acepta tanto un código pelado ("ABCD-EFGH" o
// "ABCDEFGH") como una URL del tipo
// "https://hubplay.example.com/link?code=ABCD-EFGH" — que es lo
// que /pair codifica en el QR (verification_uri_complete del
// flow RFC 8628). Devuelve null si nada parece un código.
export function extractCode(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  // Primero intentamos parsearlo como URL.
  try {
    const url = new URL(trimmed);
    const fromQuery = url.searchParams.get("code");
    if (fromQuery && isLikelyCode(fromQuery)) return fromQuery;
  } catch {
    // No era URL — sigue al fallback de "código pelado".
  }
  // Fallback: si el QR contiene directamente el código sin envoltorio
  // (caso defensivo — no es lo que /pair genera, pero no rechazamos
  // por si alguien copia el código a un QR manualmente).
  if (isLikelyCode(trimmed)) return trimmed;
  return null;
}

// isLikelyCode hace un check laxo de "esto parece un user_code"
// para descartar QRs claramente no-relevantes (URLs a youtube,
// vCards, etc.) sin replicar el alfabeto exacto del backend
// (ABCDEFGHJKMNPQRTUVWXYZ234679). Aceptamos también "ABCD-EFGH"
// con guión típico de UX. La validación real la hace el backend
// en /auth/device/approve.
function isLikelyCode(s: string): boolean {
  const stripped = s.replace(/[\s-]/g, "");
  if (stripped.length !== 8) return false;
  return /^[A-Z0-9]+$/i.test(stripped);
}
