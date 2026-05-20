// PlaybackSettings — the Settings → "Reproducción" panel.
//
// Currently houses one toggle (auto-play trailers) but kept as its own
// section because the playback area will grow (default-quality picker,
// audio-track preference, subtitle defaults) and a dedicated component
// keeps Settings.tsx from growing into a god page again.
//
// The toggle persists to /me/preferences via useUserPreference so the
// choice follows the user across devices (laptop preference reaches
// phone on next page load) — same model as HomeLayoutSettings.

import { useTranslation } from "react-i18next";
import { useUserPreference } from "@/api/hooks";
import {
  PLAYBACK_LANGUAGES,
  PREFERRED_AUDIO_LANG_PREF_KEY,
  PREFERRED_SUBTITLE_LANG_PREF_KEY,
  TRAILERS_ENABLED_PREF_KEY,
} from "@/utils/playbackPrefs";

export function PlaybackSettings() {
  const { t, i18n } = useTranslation();
  const [trailersEnabled, setTrailersEnabled] = useUserPreference<boolean>(
    TRAILERS_ENABLED_PREF_KEY,
    true,
  );
  const [preferredAudio, setPreferredAudio] = useUserPreference<string>(
    PREFERRED_AUDIO_LANG_PREF_KEY,
    "",
  );
  const [preferredSubs, setPreferredSubs] = useUserPreference<string>(
    PREFERRED_SUBTITLE_LANG_PREF_KEY,
    "",
  );

  const langOptions = PLAYBACK_LANGUAGES.map((l) => ({
    value: l.code,
    label: i18n.language?.startsWith("es") ? l.labelEs : l.labelEn,
  }));

  return (
    <div className="rounded-[--radius-lg] border border-border bg-bg-card divide-y divide-border">
      <ToggleRow
        title={t("settings.playback.trailersTitle")}
        description={t("settings.playback.trailersDescription")}
        checked={trailersEnabled}
        onChange={setTrailersEnabled}
      />
      <SelectRow
        title={t("settings.playback.audioLangTitle", {
          defaultValue: "Idioma de audio por defecto",
        })}
        description={t("settings.playback.audioLangDescription", {
          defaultValue:
            "Si el archivo tiene una pista en este idioma, el reproductor la usará automáticamente.",
        })}
        value={preferredAudio}
        onChange={setPreferredAudio}
        options={langOptions}
        emptyLabel={t("settings.playback.languageAuto", {
          defaultValue: "Audio del archivo",
        })}
      />
      <SelectRow
        title={t("settings.playback.subtitleLangTitle", {
          defaultValue: "Idioma de subtítulos por defecto",
        })}
        description={t("settings.playback.subtitleLangDescription", {
          defaultValue:
            "Si hay subtítulos en este idioma, se activarán al empezar la reproducción.",
        })}
        value={preferredSubs}
        onChange={setPreferredSubs}
        options={langOptions}
        emptyLabel={t("settings.playback.subtitlesOff", {
          defaultValue: "Sin subtítulos",
        })}
      />
    </div>
  );
}

interface SelectRowProps {
  title: string;
  description: string;
  value: string;
  onChange: (next: string) => void;
  options: { value: string; label: string }[];
  emptyLabel: string;
}

// Native <select> (not a Radix combo) because the option list is
// short (~12 items) and a native dropdown plays nicely with mobile
// pickers + screen readers without us having to wire focus state.
function SelectRow({ title, description, value, onChange, options, emptyLabel }: SelectRowProps) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-3">
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-text-primary">{title}</span>
        <span className="text-xs text-text-muted">{description}</span>
      </div>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={[
          "rounded-md border border-border bg-bg-elevated px-2 py-1.5",
          "text-sm text-text-primary cursor-pointer min-w-[10rem]",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent",
        ].join(" ")}
      >
        <option value="">{emptyLabel}</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  );
}

interface ToggleRowProps {
  title: string;
  description: string;
  checked: boolean;
  onChange: (next: boolean) => void;
}

// Inline switch primitive — Tailwind-only, no extra dep. Sibling
// HomeLayoutSettings uses arrow buttons instead of DnD for the same
// "no-new-deps, accessible-by-default" reason; the same logic applies
// to a switch vs a checkbox here. The implementation matches the
// pattern Plex/Jellyfin web use: a 36×20 pill that animates the knob
// horizontally on toggle.
function ToggleRow({ title, description, checked, onChange }: ToggleRowProps) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-3">
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-text-primary">{title}</span>
        <span className="text-xs text-text-muted">{description}</span>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={[
          "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors cursor-pointer",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card",
          checked ? "bg-accent" : "bg-bg-elevated",
        ].join(" ")}
      >
        <span
          className={[
            "inline-block size-4 transform rounded-full bg-white shadow-sm transition-transform",
            checked ? "translate-x-[18px]" : "translate-x-[2px]",
          ].join(" ")}
        />
      </button>
    </div>
  );
}
