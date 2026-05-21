// State + pure helpers for the livetv subform. Kept in a `.ts`
// sibling of `LiveTvFormFields.tsx` so the component file only
// exports React components — react-refresh / Fast Refresh only
// kicks in when that invariant holds.

import type { TFunction } from "i18next";
import { IPTV_ORG_PATH_BY_KIND, type LiveKind, type LiveSource } from "./constants";

export type LiveTvFormState = {
  liveSource: LiveSource;
  liveKind: LiveKind;
  liveFilter: string;
  country: string;
  livePick: string;
  m3uURL: string;
  epgURL: string;
  languageFilter: string[];
  tlsInsecure: boolean;
};

export function makeInitialLiveTvFormState(): LiveTvFormState {
  return {
    liveSource: "public",
    liveKind: "country",
    liveFilter: "",
    country: "",
    livePick: "",
    m3uURL: "",
    epgURL: "",
    languageFilter: [],
    tlsInsecure: false,
  };
}

type LiveTvFormPayload = {
  m3u_url: string;
  epg_url?: string;
  language_filter?: string[];
  tls_insecure?: boolean;
};

export type LiveTvFormResolved =
  | { ok: true; payload: LiveTvFormPayload }
  | { ok: false; error: string };

// Validates the URL shape used by both M3U and EPG fields. Mirrors
// the backend's validateHTTPURL guard — keeps the inline form error
// in sync with what would otherwise come back as a 400 from
// /admin/libraries or /admin/users/{id}/iptv-libraries.
function isHTTPURL(raw: string): boolean {
  const s = raw.trim();
  if (!s) return false;
  try {
    const u = new URL(s);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}

// Resolves the controlled state into the payload the API expects, or
// returns a localized error message ready to feed into a form-level
// alert. Pure function so callers can wire it into their own submit
// handler without coupling to mutation lifecycle.
export function resolveLiveTvForm(
  state: LiveTvFormState,
  t: TFunction,
): LiveTvFormResolved {
  let m3u = "";
  if (state.liveSource === "public") {
    const pick = state.liveKind === "country" ? state.country : state.livePick;
    if (!pick) {
      return {
        ok: false,
        error: t("admin.libraries.errors.livePickRequired", {
          defaultValue:
            "Selecciona un país, categoría, idioma o región para la lista pública.",
        }),
      };
    }
    m3u = `https://iptv-org.github.io/iptv/${IPTV_ORG_PATH_BY_KIND[state.liveKind]}/${pick}.m3u`;
  } else {
    if (!state.m3uURL.trim()) {
      return {
        ok: false,
        error: t("admin.libraries.errors.m3uRequired", {
          defaultValue: "La URL del M3U es obligatoria.",
        }),
      };
    }
    if (!isHTTPURL(state.m3uURL)) {
      return {
        ok: false,
        error: t("admin.libraries.errors.m3uInvalid", {
          defaultValue: "La URL del M3U debe empezar por http:// o https://.",
        }),
      };
    }
    m3u = state.m3uURL.trim();
  }

  const epg = state.epgURL.trim();
  if (epg && !isHTTPURL(epg)) {
    return {
      ok: false,
      error: t("admin.libraries.errors.epgInvalid", {
        defaultValue: "La URL del EPG debe empezar por http:// o https://.",
      }),
    };
  }

  return {
    ok: true,
    payload: {
      m3u_url: m3u,
      epg_url: epg || undefined,
      language_filter:
        state.languageFilter.length > 0 ? state.languageFilter : undefined,
      tls_insecure: state.tlsInsecure || undefined,
    },
  };
}
