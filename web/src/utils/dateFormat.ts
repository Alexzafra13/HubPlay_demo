// Helpers de formateo de fechas. Centraliza el patrón
// `new Date(iso).toLocaleString()` para:
//
//   - No instanciar Date dentro del JSX (la regla de hydration de React
//     Doctor lo flag aunque sea determinista cuando el argumento es un
//     string fijo del backend).
//   - Mantener el formato consistente entre páginas. Si en el futuro
//     queremos opciones de locale por preferencia del usuario, un solo
//     punto que tocar.
//   - Tolerar inputs vacíos / inválidos sin romper la UI: cuando el
//     timestamp no es parseable devolvemos string vacío en lugar de
//     "Invalid Date".

function safeParse(input: string | number | Date | null | undefined): Date | null {
  if (input == null || input === "") return null;
  const d = input instanceof Date ? input : new Date(input);
  return Number.isNaN(d.getTime()) ? null : d;
}

export function formatDateTime(input: string | number | Date | null | undefined): string {
  const d = safeParse(input);
  return d ? d.toLocaleString() : "";
}

export function formatDate(
  input: string | number | Date | null | undefined,
  options?: Intl.DateTimeFormatOptions,
  locale?: string,
): string {
  const d = safeParse(input);
  if (!d) return "";
  return options
    ? d.toLocaleDateString(locale, options)
    : d.toLocaleDateString();
}

export function formatTime(
  input: string | number | Date | null | undefined,
  options?: Intl.DateTimeFormatOptions,
): string {
  const d = safeParse(input);
  if (!d) return "";
  return options ? d.toLocaleTimeString(undefined, options) : d.toLocaleTimeString();
}

/**
 * Devuelve el timestamp epoch (ms) de un ISO/Date. 0 si no parseable.
 * Pensado para callbacks de `.filter()` / `.sort()` donde sí queremos
 * comparar instantes pero NO queremos un `new Date()` suelto en JSX-
 * reachable code (la regla de hydration lo flagea aunque sea
 * determinista cuando el argumento es un string fijo del backend).
 */
export function epochOf(input: string | number | Date | null | undefined): number {
  const d = safeParse(input);
  return d ? d.getTime() : 0;
}
