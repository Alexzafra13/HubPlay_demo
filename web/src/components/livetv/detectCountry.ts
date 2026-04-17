// Maps the browser's detected timezone to an ISO-3166 alpha-2 country code
// so the IPTV country picker can pre-select the most likely match. Silent
// fallback to the locale's region suffix (e.g. "es-ES" → "es"), then "us".
//
// Extracted from LiveTV.tsx so the map stays isolated and testable — adding
// a country here is a one-line change that doesn't require touching the main
// page component.

const tzToCountry: Record<string, string> = {
  "Europe/Madrid": "es", "Europe/London": "gb", "Europe/Paris": "fr",
  "Europe/Berlin": "de", "Europe/Rome": "it", "Europe/Lisbon": "pt",
  "Europe/Amsterdam": "nl", "Europe/Brussels": "be", "Europe/Zurich": "ch",
  "Europe/Vienna": "at", "Europe/Warsaw": "pl", "Europe/Stockholm": "se",
  "Europe/Oslo": "no", "Europe/Copenhagen": "dk", "Europe/Helsinki": "fi",
  "Europe/Dublin": "ie", "Europe/Athens": "gr", "Europe/Bucharest": "ro",
  "Europe/Prague": "cz", "Europe/Budapest": "hu", "Europe/Sofia": "bg",
  "Europe/Zagreb": "hr", "Europe/Belgrade": "rs", "Europe/Istanbul": "tr",
  "Europe/Moscow": "ru", "Europe/Kiev": "ua", "Europe/Minsk": "by",
  "America/New_York": "us", "America/Chicago": "us", "America/Denver": "us",
  "America/Los_Angeles": "us", "America/Mexico_City": "mx",
  "America/Sao_Paulo": "br", "America/Argentina/Buenos_Aires": "ar",
  "America/Bogota": "co", "America/Lima": "pe", "America/Santiago": "cl",
  "America/Caracas": "ve", "America/Toronto": "ca", "America/Vancouver": "ca",
  "Asia/Tokyo": "jp", "Asia/Shanghai": "cn", "Asia/Seoul": "kr",
  "Asia/Kolkata": "in", "Asia/Bangkok": "th", "Asia/Singapore": "sg",
  "Asia/Jakarta": "id", "Asia/Manila": "ph", "Asia/Taipei": "tw",
  "Asia/Dubai": "ae", "Asia/Riyadh": "sa", "Asia/Tehran": "ir",
  "Australia/Sydney": "au", "Pacific/Auckland": "nz",
  "Africa/Cairo": "eg", "Africa/Lagos": "ng", "Africa/Johannesburg": "za",
  "Atlantic/Canary": "es",
};

export function detectCountryCode(): string {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (tzToCountry[tz]) return tzToCountry[tz];
  } catch {
    // ignore — fall through to locale lookup
  }

  const lang = typeof navigator !== "undefined" ? navigator.language || "" : "";
  const parts = lang.split("-");
  if (parts.length >= 2) return parts[1].toLowerCase();
  return "us";
}
