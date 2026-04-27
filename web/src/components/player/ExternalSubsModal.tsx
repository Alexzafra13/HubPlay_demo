import { useState, useEffect, useCallback } from "react";
import type { FC } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api/client";
import type { ExternalSubtitleResult } from "@/api/types";

interface ExternalSubsModalProps {
  itemId: string;
  /** Pre-fill language filter (e.g. ["es", "en"]). Empty = no filter. */
  preferredLangs?: string[];
  /** Called when the user picks a candidate; the parent persists it
   *  and injects a `<track>` element into the video. */
  onSelect: (pick: ExternalSubtitleResult) => void;
  onClose: () => void;
}

/**
 * ExternalSubsModal lets the user search OpenSubtitles (or any other
 * registered subtitle provider) for the current item and pick a
 * candidate. The picked result is handed back to the parent — this
 * component does NOT touch the video element.
 *
 * UX is deliberately minimal: a language pill row that the user can
 * toggle, a debounced auto-search on language change, and a flat list
 * of results sorted by score. No previews — the player handles that
 * after the user picks one.
 */
const ExternalSubsModal: FC<ExternalSubsModalProps> = ({
  itemId,
  preferredLangs = ["es", "en"],
  onSelect,
  onClose,
}) => {
  const { t } = useTranslation();
  const [langs, setLangs] = useState<string[]>(preferredLangs);
  const [results, setResults] = useState<ExternalSubtitleResult[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const search = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await api.searchExternalSubtitles(itemId, langs);
      // Backend already sorts by score; we just trim to a sane upper
      // bound so the list isn't a 200-item wall.
      setResults(data.slice(0, 50));
    } catch (e) {
      // 503 (provider not configured) is a common case — surface it
      // distinctly so the user knows it's setup, not a transient error.
      const msg = e instanceof Error ? e.message : String(e);
      if (msg.includes("503") || msg.includes("PROVIDERS_UNAVAILABLE")) {
        setError(t("externalSubs.providersDisabled"));
      } else {
        setError(t("externalSubs.searchFailed"));
      }
      setResults(null);
    } finally {
      setLoading(false);
    }
  }, [itemId, langs, t]);

  // Initial search + re-search on language toggle. 250 ms debounce
  // so a user clicking through three languages doesn't fire three
  // requests; only the final state hits the backend.
  useEffect(() => {
    const id = window.setTimeout(search, 250);
    return () => window.clearTimeout(id);
  }, [search]);

  // Esc closes — same convention as UpNextOverlay.
  useEffect(() => {
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onEsc);
    return () => document.removeEventListener("keydown", onEsc);
  }, [onClose]);

  const toggleLang = (lang: string) => {
    setLangs((prev) =>
      prev.includes(lang) ? prev.filter((l) => l !== lang) : [...prev, lang],
    );
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-label={t("externalSubs.title")}
        className="w-full max-w-lg max-h-[80vh] flex flex-col rounded-[--radius-lg] border border-border bg-bg-card shadow-2xl shadow-black/50 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-border">
          <h2 className="text-sm font-semibold text-text-primary">
            {t("externalSubs.title")}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded text-text-secondary hover:text-text-primary hover:bg-bg-elevated cursor-pointer"
            aria-label={t("common.close")}
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Language chips. Hard-coded to a small set covers >90% of
            real-world use; an "Any" toggle would just bloat the list
            without giving the user information. */}
        <div className="flex gap-2 px-5 py-3 border-b border-border">
          {(["es", "en", "fr", "de", "it", "pt"] as const).map((l) => {
            const active = langs.includes(l);
            return (
              <button
                key={l}
                type="button"
                onClick={() => toggleLang(l)}
                className={[
                  "px-3 py-1 rounded-full text-xs font-medium transition-colors cursor-pointer",
                  active
                    ? "bg-accent text-white"
                    : "bg-bg-elevated text-text-secondary hover:text-text-primary",
                ].join(" ")}
              >
                {l.toUpperCase()}
              </button>
            );
          })}
        </div>

        {/* Result list */}
        <div className="flex-1 overflow-y-auto">
          {loading && (
            <div className="px-5 py-8 text-center text-sm text-text-muted">
              {t("common.loading")}
            </div>
          )}
          {error && (
            <div className="px-5 py-8 text-center text-sm text-error">
              {error}
            </div>
          )}
          {!loading && !error && results && results.length === 0 && (
            <div className="px-5 py-8 text-center text-sm text-text-muted">
              {t("externalSubs.noResults")}
            </div>
          )}
          {!loading && !error && results && results.length > 0 && (
            <ul className="divide-y divide-border">
              {results.map((r) => (
                <li key={`${r.source}:${r.file_id}`}>
                  <button
                    type="button"
                    onClick={() => onSelect(r)}
                    className="w-full flex items-center gap-3 px-5 py-3 text-left hover:bg-bg-elevated transition-colors cursor-pointer"
                  >
                    <span className="px-2 py-0.5 rounded text-[10px] font-bold uppercase bg-accent/20 text-accent">
                      {r.language}
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm text-text-primary truncate">
                        {r.source}
                      </p>
                      <p className="text-xs text-text-muted">
                        {r.format.toUpperCase()} · {t("externalSubs.score", { score: r.score.toFixed(1) })}
                      </p>
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
};

export { ExternalSubsModal };
export type { ExternalSubsModalProps };
