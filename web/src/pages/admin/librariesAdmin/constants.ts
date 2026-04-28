// Static catalogues used by the LibrariesAdmin page and its
// sub-components. Pure data — pure functions live in `./helpers.ts`.
//
// IPTV_ORG_* tables come from the iptv-org playlist project. Countries
// are loaded live (it's a long tail that rotates); the other three are
// stable enough to hardcode and ship in the bundle.
//
// URL patterns:
//   /iptv/countries/{code}.m3u     (code = ISO 3166-1 alpha-2, lowercase)
//   /iptv/categories/{slug}.m3u
//   /iptv/languages/{slug}.m3u     (slug = ISO 639-3)
//   /iptv/regions/{slug}.m3u

import type { ContentType } from "@/api/types";

export const IPTV_ORG_CATEGORIES: { code: string; name: string }[] = [
  { code: "general", name: "General" },
  { code: "news", name: "Informativos" },
  { code: "sports", name: "Deportes" },
  { code: "movies", name: "Películas" },
  { code: "series", name: "Series" },
  { code: "music", name: "Música" },
  { code: "kids", name: "Infantiles" },
  { code: "documentary", name: "Documentales" },
  { code: "entertainment", name: "Entretenimiento" },
  { code: "comedy", name: "Comedia" },
  { code: "business", name: "Negocios" },
  { code: "education", name: "Educación" },
  { code: "lifestyle", name: "Estilo de vida" },
  { code: "travel", name: "Viajes" },
  { code: "weather", name: "Tiempo" },
  { code: "science", name: "Ciencia" },
  { code: "religious", name: "Religioso" },
  { code: "shop", name: "Shopping" },
  { code: "cooking", name: "Cocina" },
  { code: "auto", name: "Motor" },
  { code: "animation", name: "Animación" },
  { code: "classic", name: "Clásicos" },
  { code: "family", name: "Familiar" },
  { code: "legislative", name: "Legislativo" },
  { code: "outdoor", name: "Exterior" },
  { code: "relax", name: "Relax" },
  { code: "xxx", name: "Adultos" },
];

export const IPTV_ORG_LANGUAGES: { code: string; name: string }[] = [
  { code: "spa", name: "Español" },
  { code: "cat", name: "Catalán" },
  { code: "glg", name: "Gallego" },
  { code: "eus", name: "Euskera" },
  { code: "eng", name: "English" },
  { code: "por", name: "Portugués" },
  { code: "fra", name: "Français" },
  { code: "deu", name: "Deutsch" },
  { code: "ita", name: "Italiano" },
  { code: "nld", name: "Nederlands" },
  { code: "rus", name: "Русский" },
  { code: "ara", name: "العربية" },
  { code: "tur", name: "Türkçe" },
  { code: "pol", name: "Polski" },
  { code: "ell", name: "Ελληνικά" },
  { code: "ron", name: "Română" },
  { code: "ces", name: "Čeština" },
  { code: "hun", name: "Magyar" },
  { code: "swe", name: "Svenska" },
  { code: "nor", name: "Norsk" },
  { code: "dan", name: "Dansk" },
  { code: "fin", name: "Suomi" },
  { code: "ukr", name: "Українська" },
  { code: "heb", name: "עברית" },
  { code: "hin", name: "हिन्दी" },
  { code: "cmn", name: "中文 (Mandarin)" },
  { code: "jpn", name: "日本語" },
  { code: "kor", name: "한국어" },
  { code: "tha", name: "ภาษาไทย" },
  { code: "vie", name: "Tiếng Việt" },
];

export const IPTV_ORG_REGIONS: { code: string; name: string }[] = [
  { code: "eur", name: "Europa" },
  { code: "amer", name: "América" },
  { code: "nam", name: "Norteamérica" },
  { code: "latam", name: "Latinoamérica" },
  { code: "afr", name: "África" },
  { code: "asia", name: "Asia" },
  { code: "seasia", name: "Sudeste Asiático" },
  { code: "oce", name: "Oceanía" },
  { code: "mena", name: "Oriente Medio y Norte de África" },
  { code: "carib", name: "Caribe" },
  { code: "nord", name: "Países Nórdicos" },
];

export const CONTENT_TYPES: { value: ContentType; key: string }[] = [
  { value: "movies", key: "contentTypes.movies" },
  { value: "shows", key: "contentTypes.tvShows" },
  { value: "livetv", key: "contentTypes.liveTV" },
];

export type LiveSource = "public" | "custom";
export type LiveKind = "country" | "category" | "language" | "region";

// IPTV-ORG URL family map keyed by `LiveKind`. Lives next to the slug
// tables so adding a new family is one diff (extend the kind union,
// add a row here, add a tab to the picker).
export const IPTV_ORG_PATH_BY_KIND: Record<LiveKind, string> = {
  country: "countries",
  category: "categories",
  language: "languages",
  region: "regions",
};

// Section descriptors for the libraries page. Movies / Series / Live TV
// each get their own coloured collapsible header so the three categories
// are obvious at a glance — amber for movies, cyan for series, red for
// livetv (palette pulled from globals.css). Tailwind classes baked in
// because Tailwind v4's JIT can't see colour tokens that come from
// string concat at runtime.
export const LIBRARY_SECTIONS: {
  type: ContentType;
  labelKey: string;
  headerClass: string;
  dotClass: string;
  textClass: string;
}[] = [
  {
    type: "movies",
    labelKey: "contentTypes.movies",
    headerClass: "bg-warning/5 border-warning/30 hover:bg-warning/10",
    dotClass: "bg-warning",
    textClass: "text-warning",
  },
  {
    type: "shows",
    labelKey: "contentTypes.tvShows",
    headerClass: "bg-accent-light/5 border-accent-light/30 hover:bg-accent-light/10",
    dotClass: "bg-accent-light",
    textClass: "text-accent-light",
  },
  {
    type: "livetv",
    labelKey: "contentTypes.liveTV",
    headerClass: "bg-live/5 border-live/30 hover:bg-live/10",
    dotClass: "bg-live",
    textClass: "text-live",
  },
];

