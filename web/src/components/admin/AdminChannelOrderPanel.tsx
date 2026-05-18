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
  useReplaceLibraryChannelOrder,
  useResetLibraryChannelOrder,
} from "@/api/hooks";
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
    const orderedIDs = draft.map((c) => c.id);
    const hiddenIDs = draft.filter((c) => c.hidden).map((c) => c.id);
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

  return (
    <>
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
