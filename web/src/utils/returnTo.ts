// returnTo — "vuelve a donde ibas" tras el login.
//
// ProtectedRoute guarda en location.state.from la ruta pedida (path +
// query) cuando manda a /login; Login y WhoIsWatching la reenvían y
// navegan ahí al terminar. Caso que lo motivó: escanear el QR de la TV
// con la cámara nativa del móvil abre /link?code=XXXX; sin sesión, el
// código se perdía en la redirección y había que teclearlo.
//
// Solo se aceptan rutas internas ("/algo"): nunca "//host" ni "/\host"
// (el navegador las trata como otra web → open redirect), ni rutas que
// devolverían al propio flujo de login.

interface ReturnToState {
  from?: string;
}

const AUTH_FLOW = ["/login", "/select-profile", "/change-password", "/setup"];

export function safeReturnTo(state: unknown): string {
  const from =
    state && typeof state === "object" && "from" in state
      ? (state as ReturnToState).from
      : undefined;
  if (typeof from !== "string" || !from.startsWith("/")) return "/";
  if (from.startsWith("//") || from.startsWith("/\\")) return "/";
  const path = from.split(/[?#]/)[0];
  if (AUTH_FLOW.some((p) => path === p || path.startsWith(p + "/"))) return "/";
  return from;
}
