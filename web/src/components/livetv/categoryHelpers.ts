// M3U group-title fields arrive noisy: "News;Public", "MOVIES / Drama",
// "Sports,HD", "Noticias|24h". We split on the common separators, dedupe,
// drop empties, and title-case each part. The first token becomes the
// "primary" category used for icons and grouping.

const SEP_RE = /[;,/|]+/;

// Placeholder values that real M3U feeds use when the author didn't classify
// the channel — we collapse them all into a single "Otros" bucket so the
// UI doesn't surface technical-looking strings like "Undefined".
const UNCLASSIFIED_RE =
  /^(undefined|uncategori[sz]ed|unknown|n\/?a|none|null|miscellaneous|misc|other|otros)$/i;
const UNCLASSIFIED_KEY = "Otros";

export interface ParsedCategory {
  primary: string; // canonical primary category ("News", "Movies"…)
  all: string[]; // every token, title-cased & deduped
  raw: string | null; // original string (for tooltips/debug)
}

function titleCase(s: string): string {
  const trimmed = s.trim();
  if (!trimmed) return "";
  return trimmed
    .toLocaleLowerCase()
    .replace(/\b[\p{L}][\p{L}'’]*/gu, (w) => w[0].toLocaleUpperCase() + w.slice(1));
}

function normalizeToken(s: string): string {
  const cased = titleCase(s);
  if (!cased) return "";
  return UNCLASSIFIED_RE.test(cased) ? UNCLASSIFIED_KEY : cased;
}

export function parseCategory(group: string | null | undefined): ParsedCategory {
  if (!group) return { primary: UNCLASSIFIED_KEY, all: [], raw: null };
  const parts = group
    .split(SEP_RE)
    .map(normalizeToken)
    .filter(Boolean);
  const seen = new Set<string>();
  const unique: string[] = [];
  for (const p of parts) {
    const key = p.toLocaleLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    unique.push(p);
  }
  return {
    primary: unique[0] ?? UNCLASSIFIED_KEY,
    all: unique,
    raw: group,
  };
}

/**
 * True when a category is the "unclassified" bucket — useful for sorting
 * it to the end of the category rail regardless of channel count.
 */
export function isUnclassifiedCategory(name: string): boolean {
  return name === UNCLASSIFIED_KEY;
}

// Visual metadata per category. We match against a set of normalized keywords
// so "Noticias", "News", "Information", "Info" all resolve to the same bucket.
// Fallback keeps things working for unknown M3U groups.

export interface CategoryMeta {
  key: string;
  icon: string; // single emoji — renders cross-platform without an icon font
  /** Tailwind classes for a subtle tinted background + foreground pair. */
  tint: string;
  /** A stronger accent, for active chips and "now airing" highlights. */
  accent: string;
}

const FALLBACK: CategoryMeta = {
  key: "general",
  icon: "📺",
  tint: "bg-slate-500/10 text-slate-300",
  accent: "bg-slate-500/25 text-slate-100 ring-slate-400/40",
};

const CATEGORY_TABLE: Array<{ match: RegExp; meta: CategoryMeta }> = [
  {
    match: /^otros$/i,
    meta: {
      key: "otros",
      icon: "🗂️",
      tint: "bg-zinc-500/10 text-zinc-300",
      accent: "bg-zinc-500/25 text-zinc-100 ring-zinc-400/40",
    },
  },
  {
    match: /news|noticia|info(rmaci[oó]n)?|actual/i,
    meta: {
      key: "news",
      icon: "📰",
      tint: "bg-sky-500/10 text-sky-300",
      accent: "bg-sky-500/25 text-sky-100 ring-sky-400/40",
    },
  },
  {
    match: /sport|deport|f[uú]tbol|futbol|soccer|nba|nfl|mlb/i,
    meta: {
      key: "sports",
      icon: "⚽",
      tint: "bg-emerald-500/10 text-emerald-300",
      accent: "bg-emerald-500/25 text-emerald-100 ring-emerald-400/40",
    },
  },
  {
    match: /movie|cine|film|pel[ií]cula/i,
    meta: {
      key: "movies",
      icon: "🎬",
      tint: "bg-amber-500/10 text-amber-300",
      accent: "bg-amber-500/25 text-amber-100 ring-amber-400/40",
    },
  },
  {
    match: /series|tv show|drama|novela/i,
    meta: {
      key: "series",
      icon: "🎞️",
      tint: "bg-orange-500/10 text-orange-300",
      accent: "bg-orange-500/25 text-orange-100 ring-orange-400/40",
    },
  },
  {
    match: /kids|ni[ñn]os|cartoon|anim|infantil/i,
    meta: {
      key: "kids",
      icon: "🧸",
      tint: "bg-pink-500/10 text-pink-300",
      accent: "bg-pink-500/25 text-pink-100 ring-pink-400/40",
    },
  },
  {
    match: /music|m[uú]sica|mtv/i,
    meta: {
      key: "music",
      icon: "🎵",
      tint: "bg-purple-500/10 text-purple-300",
      accent: "bg-purple-500/25 text-purple-100 ring-purple-400/40",
    },
  },
  {
    match: /documentar|history|science|ciencia|cultur/i,
    meta: {
      key: "docs",
      icon: "🌍",
      tint: "bg-teal-500/10 text-teal-300",
      accent: "bg-teal-500/25 text-teal-100 ring-teal-400/40",
    },
  },
  {
    match: /weather|tiempo|meteo|cl(i|í)ma/i,
    meta: {
      key: "weather",
      icon: "⛅",
      tint: "bg-cyan-500/10 text-cyan-300",
      accent: "bg-cyan-500/25 text-cyan-100 ring-cyan-400/40",
    },
  },
  {
    match: /comedy|humor|entertain|variety|entreten/i,
    meta: {
      key: "entertainment",
      icon: "🎭",
      tint: "bg-fuchsia-500/10 text-fuchsia-300",
      accent: "bg-fuchsia-500/25 text-fuchsia-100 ring-fuchsia-400/40",
    },
  },
  {
    match: /relig|faith|church|cristian|cat[oó]l/i,
    meta: {
      key: "religion",
      icon: "🕊️",
      tint: "bg-indigo-500/10 text-indigo-300",
      accent: "bg-indigo-500/25 text-indigo-100 ring-indigo-400/40",
    },
  },
  {
    match: /public|gener(al|ista)|nacional/i,
    meta: {
      key: "public",
      icon: "📡",
      tint: "bg-blue-500/10 text-blue-300",
      accent: "bg-blue-500/25 text-blue-100 ring-blue-400/40",
    },
  },
];

export function categoryMeta(category: string): CategoryMeta {
  for (const { match, meta } of CATEGORY_TABLE) {
    if (match.test(category)) return meta;
  }
  return FALLBACK;
}
