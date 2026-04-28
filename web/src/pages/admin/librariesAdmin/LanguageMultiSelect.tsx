import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";

/**
 * Common ISO 639-1 codes shown as one-tap chips. The list is the
 * intersection of "languages our users actually speak" and "languages
 * the matcher's group-keyword patterns recognise". Extending the
 * matcher means extending this list — keep them in sync.
 */
const COMMON_LANGUAGES: ReadonlyArray<{ code: string; label: string }> = [
  { code: "es", label: "Español" },
  { code: "en", label: "English" },
  { code: "fr", label: "Français" },
  { code: "de", label: "Deutsch" },
  { code: "it", label: "Italiano" },
  { code: "pt", label: "Português" },
  { code: "ru", label: "Русский" },
  { code: "ar", label: "العربية" },
  { code: "tr", label: "Türkçe" },
  { code: "pl", label: "Polski" },
  { code: "nl", label: "Nederlands" },
  { code: "el", label: "Ελληνικά" },
];

interface Props {
  value: string[];
  onChange: (next: string[]) => void;
}

/**
 * Language allowlist editor for the M3U import. An empty selection
 * means "no filter" (every channel is imported), which is the
 * historical default — the parent component decides how to surface
 * that distinction in copy.
 *
 * Backed by a chip toggle for the common languages plus a small
 * free-text input that accepts any 2–3 letter ISO code so users with
 * exotic feeds aren't boxed in.
 */
export function LanguageMultiSelect({ value, onChange }: Props) {
  const { t } = useTranslation();
  const [custom, setCustom] = useState("");

  const selected = new Set(value);

  function toggle(code: string) {
    const next = new Set(selected);
    if (next.has(code)) next.delete(code);
    else next.add(code);
    onChange([...next].sort());
  }

  function handleAddCustom(e: FormEvent) {
    e.preventDefault();
    const code = custom.trim().toLowerCase();
    if (!/^[a-z]{2,3}$/.test(code)) return;
    if (!selected.has(code)) onChange([...selected, code].sort());
    setCustom("");
  }

  return (
    <div className="space-y-2">
      <label className="block text-xs font-medium text-text-muted">
        {t("admin.libraries.languageFilter", {
          defaultValue: "Filtrar idiomas (opcional)",
        })}
      </label>
      <div className="flex flex-wrap gap-1.5">
        {COMMON_LANGUAGES.map(({ code, label }) => {
          const active = selected.has(code);
          return (
            <button
              type="button"
              key={code}
              onClick={() => toggle(code)}
              aria-pressed={active}
              className={`px-2.5 py-1 text-xs rounded-full border transition-colors ${
                active
                  ? "bg-accent text-white border-accent"
                  : "bg-bg-1 text-text-muted border-border hover:border-text-muted"
              }`}
            >
              {label}
              <span className="ml-1 text-[10px] opacity-70">{code}</span>
            </button>
          );
        })}
        {/* Custom codes already selected but not in the common list */}
        {[...selected]
          .filter((code) => !COMMON_LANGUAGES.some((l) => l.code === code))
          .map((code) => (
            <button
              type="button"
              key={code}
              onClick={() => toggle(code)}
              className="px-2.5 py-1 text-xs rounded-full border bg-accent text-white border-accent"
            >
              {code} ✕
            </button>
          ))}
      </div>
      <form onSubmit={handleAddCustom} className="flex items-center gap-2">
        <input
          type="text"
          value={custom}
          onChange={(e) => setCustom(e.target.value)}
          placeholder={t("admin.libraries.languageFilterCustom", {
            defaultValue: "Otro código (ej. zh)",
          })}
          maxLength={3}
          className="px-2 py-1 text-xs rounded border border-border bg-bg-1 text-text w-32"
        />
        <button
          type="submit"
          className="px-2 py-1 text-xs rounded border border-border text-text-muted hover:text-text"
        >
          {t("common.add", { defaultValue: "Añadir" })}
        </button>
      </form>
      <p className="text-[11px] text-text-muted">
        {selected.size === 0
          ? t("admin.libraries.languageFilterEmpty", {
              defaultValue:
                "Sin selección: se importan todos los idiomas (comportamiento por defecto).",
            })
          : t("admin.libraries.languageFilterHint", {
              defaultValue:
                "Solo se persisten canales cuyo tvg-language, tvg-country, group-title o nombre coincida.",
            })}
      </p>
    </div>
  );
}
