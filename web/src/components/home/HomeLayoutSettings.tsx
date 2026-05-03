// HomeLayoutSettings — the Settings → "Inicio" panel.
//
// Lists every section the user's home page can render (continue
// watching, next up, trending, live now, plus one "Latest in X" per
// library) and lets the viewer:
//
//   - Toggle each section on/off
//   - Move sections up / down with arrow buttons
//   - Reset to the server-generated default
//
// Why arrow buttons instead of drag-and-drop: zero new dependencies,
// accessible by keyboard out of the box, works on touch without
// extra hit-target tuning, and the typical home has 5-8 sections —
// fast enough to reorder without DnD.
//
// Persistence model: every change calls usePutHomeLayout immediately
// and optimistically updates the React Query cache, so the home
// page mirrors the change on next mount without waiting for the
// roundtrip. Failures revert the optimistic patch via the
// mutation's onError.

import { useTranslation } from "react-i18next";
import { Trans } from "react-i18next";
import {
  useHomeLayout,
  usePutHomeLayout,
} from "@/api/hooks";
import type { HomeLayout, HomeSection } from "@/api/types";
import { Button, Spinner } from "@/components/common";

export function HomeLayoutSettings() {
  const { t } = useTranslation();
  const { data: layout, isLoading } = useHomeLayout();
  const putLayout = usePutHomeLayout();

  const apply = (next: HomeLayout) => {
    putLayout.mutate(next);
  };

  const toggleVisible = (id: string) => {
    if (!layout) return;
    apply({
      ...layout,
      sections: layout.sections.map((s) =>
        s.id === id ? { ...s, visible: !s.visible } : s,
      ),
    });
  };

  const move = (idx: number, delta: -1 | 1) => {
    if (!layout) return;
    const newIdx = idx + delta;
    if (newIdx < 0 || newIdx >= layout.sections.length) return;
    const next = [...layout.sections];
    [next[idx], next[newIdx]] = [next[newIdx], next[idx]];
    apply({ ...layout, sections: next });
  };

  const resetDefaults = () => {
    if (!layout) return;
    // Sending an empty section list signals "blow away my custom
    // layout" — the GET endpoint then re-derives the default from
    // the user's libraries on the next read. The frontend
    // immediately invalidates the layout query so the new defaults
    // come back without a manual refresh.
    apply({ version: 1, sections: [] });
  };

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    );
  }

  if (!layout) return null;

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-text-muted">
        <Trans
          i18nKey="settings.homeLayout.intro"
          defaults="Reordena y activa/desactiva las secciones que aparecen en tu página de Inicio. Los cambios se guardan al instante y se aplican solo a tu cuenta."
        />
      </p>

      <ol
        className="rounded-[--radius-lg] border border-border bg-bg-card divide-y divide-border"
        aria-label={t("settings.homeLayout.listLabel", {
          defaultValue: "Secciones del inicio",
        })}
      >
        {layout.sections.map((section, idx) => (
          <SectionRow
            key={section.id}
            section={section}
            index={idx}
            total={layout.sections.length}
            onMoveUp={() => move(idx, -1)}
            onMoveDown={() => move(idx, +1)}
            onToggle={() => toggleVisible(section.id)}
            disabled={putLayout.isPending}
          />
        ))}
      </ol>

      <div className="flex items-center justify-between">
        <p className="text-xs text-text-muted">
          {putLayout.isPending
            ? t("settings.homeLayout.saving", { defaultValue: "Guardando..." })
            : putLayout.isError
              ? t("settings.homeLayout.saveFailed", {
                  defaultValue: "No se pudo guardar — reintenta el cambio.",
                })
              : t("settings.homeLayout.savedHint", {
                  defaultValue: "Los cambios se guardan automáticamente.",
                })}
        </p>
        <Button variant="secondary" size="sm" onClick={resetDefaults} disabled={putLayout.isPending}>
          {t("settings.homeLayout.reset", {
            defaultValue: "Restaurar valores por defecto",
          })}
        </Button>
      </div>
    </div>
  );
}

interface SectionRowProps {
  section: HomeSection;
  index: number;
  total: number;
  onMoveUp: () => void;
  onMoveDown: () => void;
  onToggle: () => void;
  disabled: boolean;
}

function SectionRow({
  section,
  index,
  total,
  onMoveUp,
  onMoveDown,
  onToggle,
  disabled,
}: SectionRowProps) {
  const { t } = useTranslation();
  const label = sectionLabel(t, section);
  const isFirst = index === 0;
  const isLast = index === total - 1;

  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="flex flex-col">
        <button
          type="button"
          onClick={onMoveUp}
          disabled={disabled || isFirst}
          aria-label={t("settings.homeLayout.moveUp", {
            defaultValue: "Mover arriba",
          })}
          className="rounded p-1 text-text-secondary hover:text-text-primary hover:bg-bg-elevated disabled:opacity-30 disabled:hover:bg-transparent transition-colors"
        >
          <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 12l5-5 5 5" />
          </svg>
        </button>
        <button
          type="button"
          onClick={onMoveDown}
          disabled={disabled || isLast}
          aria-label={t("settings.homeLayout.moveDown", {
            defaultValue: "Mover abajo",
          })}
          className="rounded p-1 text-text-secondary hover:text-text-primary hover:bg-bg-elevated disabled:opacity-30 disabled:hover:bg-transparent transition-colors"
        >
          <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M5 8l5 5 5-5" />
          </svg>
        </button>
      </div>

      <SectionIcon type={section.type} />

      <div className="flex-1 min-w-0">
        <p className={`text-sm font-medium truncate ${section.visible ? "text-text-primary" : "text-text-muted line-through"}`}>
          {label}
        </p>
        <p className="text-xs text-text-muted">
          {sectionTypeLabel(t, section.type)}
        </p>
      </div>

      <Toggle checked={section.visible} onChange={onToggle} disabled={disabled} />
    </li>
  );
}

function SectionIcon({ type }: { type: HomeSection["type"] }) {
  const cls = "h-4 w-4 text-accent-light";
  switch (type) {
    case "continue_watching":
      return (
        <svg className={cls} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="10" cy="10" r="7.5" />
          <path d="M8 7l5 3-5 3z" fill="currentColor" />
        </svg>
      );
    case "next_up":
      return (
        <svg className={cls} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M5 5l5 5-5 5" />
          <path d="M11 5l5 5-5 5" />
        </svg>
      );
    case "trending":
      return (
        <svg className={cls} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M3 14l4-4 3 3 7-7" />
          <path d="M13 6h4v4" />
        </svg>
      );
    case "live_now":
      return (
        <svg className={cls} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="10" cy="10" r="3" fill="currentColor" />
          <path d="M5 5a7 7 0 000 10M15 5a7 7 0 010 10" />
        </svg>
      );
    case "latest_in_library":
      return (
        <svg className={cls} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="3" width="14" height="14" rx="1.5" />
          <path d="M3 8h14M8 3v14" />
        </svg>
      );
  }
}

function sectionTypeLabel(t: ReturnType<typeof useTranslation>["t"], type: HomeSection["type"]): string {
  switch (type) {
    case "continue_watching":
      return t("home.continueWatching");
    case "next_up":
      return t("home.nextUp");
    case "trending":
      return t("home.trending", { defaultValue: "Tendencia esta semana" });
    case "live_now":
      return t("home.liveNow", { defaultValue: "En directo ahora" });
    case "latest_in_library":
      return t("settings.homeLayout.libraryRail", {
        defaultValue: "Rail por biblioteca",
      });
  }
}

function sectionLabel(t: ReturnType<typeof useTranslation>["t"], section: HomeSection): string {
  if (section.type === "latest_in_library") {
    const name = section.library_name ?? "?";
    return t("home.latestIn", { library: name, defaultValue: `Reciente en ${name}` });
  }
  return sectionTypeLabel(t, section.type);
}

interface ToggleProps {
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
}

function Toggle({ checked, onChange, disabled }: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={onChange}
      className={[
        "relative inline-flex h-6 w-11 items-center rounded-full transition-colors flex-shrink-0",
        checked ? "bg-accent" : "bg-bg-elevated",
        disabled ? "opacity-50 cursor-not-allowed" : "cursor-pointer",
      ].join(" ")}
    >
      <span
        className={[
          "inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform",
          checked ? "translate-x-6" : "translate-x-1",
        ].join(" ")}
      />
    </button>
  );
}
