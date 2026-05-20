// AdminChannelOrderPanel — admin curation surface for a library's
// channel list. The admin reorders the default view every
// non-admin user inherits, and toggles per-channel visibility as
// a hard constraint (downstream the per-user overlay can only
// hide more, not surface what the admin removed).
//
// Mirrors LiveTvCustomize for the per-user counterpart — same
// editor component, different mutation endpoints and a different
// scope hint at the top.

import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  useAdminChannelsForOrder,
  useRefreshLogosFromIPTVOrg,
  useReplaceLibraryChannelOrder,
  useResetLibraryChannelOrder,
} from "@/api/hooks";
import { Button, Spinner } from "@/components/common";
import { Sparkles } from "lucide-react";
import { arrayMove } from "@dnd-kit/sortable";
import {
  ChannelOrderEditor,
  type DraftChannel,
} from "@/components/livetv/ChannelOrderEditor";
import { ChannelLogoEditor } from "./ChannelLogoEditor";

interface Props {
  libraryId: string;
}

export function AdminChannelOrderPanel({ libraryId }: Props) {
  const { t } = useTranslation();
  const channelsQ = useAdminChannelsForOrder(libraryId);
  const replaceOrder = useReplaceLibraryChannelOrder();
  const resetOrder = useResetLibraryChannelOrder();

  const [draft, setDraft] = useState<DraftChannel[]>([]);
  const [seededFor, setSeededFor] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [savedMessage, setSavedMessage] = useState<string | null>(null);
  // Logo override modal state. `editingLogoFor` guarda el id del canal
  // que el operador acaba de clickar; el modal se renderiza con su
  // metadata (nombre, iniciales, colores). null = modal cerrado.
  const [editingLogoFor, setEditingLogoFor] = useState<string | null>(null);

  // Render-time seed (same trick LiveTvCustomize uses): re-seed
  // only when libraryId actually changes, not on every channelsQ.data
  // ref. An effect-based version trips the react-hooks/set-state-in-
  // effect lint AND would silently clobber in-flight edits on a
  // tab-focus refetch.
  if (libraryId && seededFor !== libraryId && channelsQ.data) {
    setSeededFor(libraryId);
    setDraft(
      channelsQ.data.map((c) => ({
        id: c.id,
        name: c.name,
        group_name: c.group_name,
        hidden: !!c.hidden,
        logo_url: c.logo_url ?? undefined,
        logo_initials: c.logo_initials,
        logo_bg: c.logo_bg,
        logo_fg: c.logo_fg,
      })),
    );
    setDirty(false);
    setSavedMessage(null);
  }

  const reorder = useCallback((from: number, to: number) => {
    setDraft((d) => {
      if (from === to || from < 0 || to < 0 || from >= d.length || to >= d.length) {
        return d;
      }
      return arrayMove(d, from, to);
    });
    setDirty(true);
    setSavedMessage(null);
  }, []);

  const toggleHidden = useCallback((index: number) => {
    setDraft((d) => {
      const next = d.slice();
      next[index] = { ...next[index], hidden: !next[index].hidden };
      return next;
    });
    setDirty(true);
    setSavedMessage(null);
  }, []);

  const bulkSetHidden = useCallback((ids: string[], hidden: boolean) => {
    if (ids.length === 0) return;
    const idSet = new Set(ids);
    setDraft((d) =>
      d.map((c) => (idSet.has(c.id) ? { ...c, hidden } : c)),
    );
    setDirty(true);
    setSavedMessage(null);
  }, []);

  const handleSave = useCallback(async () => {
    // Una sola pasada para los dos arrays — antes hacíamos map + filter
    // + map (3 recorridos sobre la misma lista, hasta cientos de canales).
    const orderedIDs: string[] = [];
    const hiddenIDs: string[] = [];
    for (const c of draft) {
      orderedIDs.push(c.id);
      if (c.hidden) hiddenIDs.push(c.id);
    }
    await replaceOrder.mutateAsync({
      libraryId,
      ordered_channel_ids: orderedIDs,
      hidden_channel_ids: hiddenIDs,
    });
    setDirty(false);
    setSavedMessage(
      t("admin.livetv.channelOrder.saved", { defaultValue: "Orden guardado." }),
    );
  }, [draft, libraryId, replaceOrder, t]);

  const handleReset = useCallback(async () => {
    const msg = t("admin.livetv.channelOrder.resetConfirm", {
      defaultValue:
        "¿Restaurar el orden original del M3U? Tus reordenaciones y canales ocultos se perderán.",
    });
    if (!confirm(msg)) return;
    await resetOrder.mutateAsync(libraryId);
    const result = await channelsQ.refetch();
    if (result.data) {
      setDraft(
        result.data.map((c) => ({
          id: c.id,
          name: c.name,
          group_name: c.group_name,
          hidden: !!c.hidden,
        })),
      );
      setDirty(false);
    }
    setSavedMessage(
      t("admin.livetv.channelOrder.resetDone", {
        defaultValue: "Orden restaurado al del M3U.",
      }),
    );
  }, [channelsQ, libraryId, resetOrder, t]);

  const hint = useMemo(
    () =>
      t("admin.livetv.channelOrder.scopeHint", {
        defaultValue:
          "Este es el orden por defecto que ven todos los usuarios. Cada usuario puede personalizar el suyo encima, pero NO puede mostrar los canales que ocultes aquí.",
      }),
    [t],
  );

  const editingChannel = editingLogoFor
    ? draft.find((c) => c.id === editingLogoFor)
    : null;

  const refreshLogos = useRefreshLogosFromIPTVOrg();
  const [iptvOrgMessage, setIPTVOrgMessage] = useState<string | null>(null);

  const handleIPTVOrgRefresh = useCallback(async () => {
    setIPTVOrgMessage(null);
    try {
      const res = await refreshLogos.mutateAsync(libraryId);
      await channelsQ.refetch();
      setSeededFor(null);
      // Mensaje rico: explica el "por qué" cuando updated=0. Sin esto
      // el operador ve "0 canales actualizados" sin pista de si es
      // porque ya tenía logos, sin tvg-id, o no hay match en la base.
      if (res.updated > 0) {
        setIPTVOrgMessage(
          t("admin.livetv.channelOrder.iptvOrgDone", {
            defaultValue: "Se han añadido logos a {{count}} canales desde iptv-org.",
            count: res.updated,
          }),
        );
      } else {
        const parts: string[] = [];
        if (res.already_have_logo > 0) {
          parts.push(
            t("admin.livetv.channelOrder.iptvOrgAlreadyHave", {
              defaultValue: "{{count}} ya tenían logo del M3U",
              count: res.already_have_logo,
            }),
          );
        }
        if (res.without_tvg_id > 0) {
          parts.push(
            t("admin.livetv.channelOrder.iptvOrgNoTvgID", {
              defaultValue: "{{count}} sin tvg-id (necesario para buscar)",
              count: res.without_tvg_id,
            }),
          );
        }
        if (res.not_in_database > 0) {
          parts.push(
            t("admin.livetv.channelOrder.iptvOrgNotInDB", {
              defaultValue: "{{count}} sin coincidencia en iptv-org",
              count: res.not_in_database,
            }),
          );
        }
        if (res.skipped_has_override > 0) {
          parts.push(
            t("admin.livetv.channelOrder.iptvOrgHasOverride", {
              defaultValue: "{{count}} ya tenían override manual",
              count: res.skipped_has_override,
            }),
          );
        }
        const breakdown = parts.length > 0 ? ` (${parts.join(", ")})` : "";
        setIPTVOrgMessage(
          t("admin.livetv.channelOrder.iptvOrgNoneUpdated", {
            defaultValue: "Ningún canal actualizado{{breakdown}}.",
            breakdown,
          }),
        );
      }
    } catch {
      setIPTVOrgMessage(
        t("admin.livetv.channelOrder.iptvOrgError", {
          defaultValue: "No se ha podido contactar con iptv-org. Inténtalo de nuevo.",
        }),
      );
    }
  }, [channelsQ, libraryId, refreshLogos, t]);

  return (
    <>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-[--radius-md] border border-accent/20 bg-accent/5 px-3 py-2">
        <div className="text-xs text-text-muted">
          <strong className="text-text">iptv-org</strong> ·{" "}
          {t("admin.livetv.channelOrder.iptvOrgHint", {
            defaultValue:
              "Busca logos en la base pública de iptv-org para los canales que no tengan tvg-logo en el M3U.",
          })}
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={handleIPTVOrgRefresh}
          disabled={refreshLogos.isPending}
        >
          {refreshLogos.isPending ? (
            <Spinner size="sm" />
          ) : (
            <Sparkles className="size-3.5" />
          )}
          {t("admin.livetv.channelOrder.iptvOrgButton", {
            defaultValue: "Buscar logos en iptv-org",
          })}
        </Button>
      </div>
      {iptvOrgMessage && (
        <div
          role="status"
          className="mb-3 rounded-[--radius-sm] bg-success/10 px-3 py-2 text-sm text-success"
        >
          {iptvOrgMessage}
        </div>
      )}

      <ChannelOrderEditor
        draft={draft}
        loading={channelsQ.isLoading}
        onReorder={reorder}
        onToggleHidden={toggleHidden}
        onBulkSetHidden={bulkSetHidden}
        onEditLogo={setEditingLogoFor}
        onSave={handleSave}
        onReset={handleReset}
        dirty={dirty}
        savePending={replaceOrder.isPending}
        resetPending={resetOrder.isPending}
        savedMessage={savedMessage}
        hint={hint}
        saveLabelKey="admin.livetv.channelOrder.save"
        resetLabelKey="admin.livetv.channelOrder.reset"
        emptyTitleKey="admin.livetv.channelOrder.empty"
        emptyHintKey="admin.livetv.channelOrder.emptyHint"
      />
      {editingChannel && (
        <ChannelLogoEditor
          isOpen={true}
          onClose={() => setEditingLogoFor(null)}
          channelID={editingChannel.id}
          channelName={editingChannel.name}
          proxyLogoURL={editingChannel.logo_url ?? `/api/v1/channels/${encodeURIComponent(editingChannel.id)}/logo`}
          initials={editingChannel.logo_initials ?? "?"}
          initialsBg={editingChannel.logo_bg ?? "#1f2937"}
          initialsFg={editingChannel.logo_fg ?? "#ffffff"}
        />
      )}
    </>
  );
}
