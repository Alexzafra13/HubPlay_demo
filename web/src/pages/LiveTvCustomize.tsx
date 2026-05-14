// LiveTvCustomize — per-user Live TV personalisation panel.
//
// Lets the viewer reorder + hide channels for their own account
// without affecting the admin's defaults or other users. The list
// loads with the admin's original ordering on first mount; every
// edit stays local until the user clicks "Guardar".
//
// UX shape (v1):
//   - One library at a time (most home users have one IPTV library
//     anyway). A dropdown switches between them.
//   - Each row: drag handle (visual only for v1) + ↑↓ buttons +
//     hide toggle + channel name + group.
//   - "Restaurar orden del administrador" button wipes the user's
//     overrides via DELETE /me/iptv/channels/order.
//   - "Guardar" persists everything in one transaction.
//
// Why a separate page (vs. inline in LiveTV.tsx): the personalisation
// view shows hidden channels too (so the user can un-hide), which is
// a different filter than what the main Live TV grid wants. Sharing
// the same screen would mean a mode switch with two contradictory
// filters. A dedicated route is cleaner and easier to bookmark.

import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { ArrowLeft } from "lucide-react";

import {
  useChannelsForPersonalisation,
  useLibraries,
  useReplaceChannelOrder,
  useResetChannelOrder,
} from "@/api/hooks";
import { EmptyState } from "@/components/common";
import { ChannelOrderEditor } from "@/components/livetv/ChannelOrderEditor";

// Working copy of one channel as the user reorders / toggles. Local
// to the page — once "Guardar" persists, we refetch the canonical
// list.
interface DraftChannel {
  id: string;
  name: string;
  group_name: string | null;
  hidden: boolean;
}

export default function LiveTvCustomize() {
  const { t } = useTranslation();
  const { data: libraries } = useLibraries();
  const livetvLibs = useMemo(
    () => (libraries ?? []).filter((l) => l.content_type === "livetv"),
    [libraries],
  );

  // User-selected library; null means "fall back to the first one".
  // We derive the effective libraryId during render instead of
  // syncing it from livetvLibs in an effect — the latter trips the
  // react-hooks/set-state-in-effect lint and cascades renders.
  const [selectedLibrary, setSelectedLibrary] = useState<string | null>(null);
  const libraryId = selectedLibrary ?? livetvLibs[0]?.id ?? "";

  const channelsQ = useChannelsForPersonalisation(libraryId || undefined);
  const replaceOrder = useReplaceChannelOrder();
  const resetOrder = useResetChannelOrder();

  const [draft, setDraft] = useState<DraftChannel[]>([]);
  const [seededForLibrary, setSeededForLibrary] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [savedMessage, setSavedMessage] = useState<string | null>(null);

  // State-update-during-render: re-seed the draft only when the
  // library actually changes, not on every channelsQ.data ref. The
  // previous useEffect-based seed also fired on tab-focus refetches,
  // which silently clobbered any in-flight edits.
  if (libraryId && seededForLibrary !== libraryId) {
    if (channelsQ.data) {
      setSeededForLibrary(libraryId);
      setDraft(
        channelsQ.data.map((c) => ({
          id: c.id,
          name: c.name,
          group_name: c.group_name,
          hidden: !!c.hidden,
        })),
      );
      setDirty(false);
      setSavedMessage(null);
    } else if (draft.length > 0) {
      // Library switched but the new query hasn't resolved yet —
      // drop the previous library's rows so the spinner takes over.
      setDraft([]);
      setDirty(false);
      setSavedMessage(null);
    }
  }

  const move = useCallback((index: number, delta: number) => {
    setDraft((d) => {
      const target = index + delta;
      if (target < 0 || target >= d.length) return d;
      const next = d.slice();
      const tmp = next[index];
      next[index] = next[target];
      next[target] = tmp;
      return next;
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

  const handleSave = useCallback(async () => {
    const orderedIDs = draft.map((c) => c.id);
    const hiddenIDs = draft.filter((c) => c.hidden).map((c) => c.id);
    await replaceOrder.mutateAsync({
      ordered_channel_ids: orderedIDs,
      hidden_channel_ids: hiddenIDs,
    });
    setDirty(false);
    setSavedMessage(t("livetv.customize.saved"));
  }, [draft, replaceOrder, t]);

  const handleReset = useCallback(async () => {
    if (!confirm(t("livetv.customize.resetConfirm"))) return;
    await resetOrder.mutateAsync();
    // The mutation invalidated the personalise query; refetch the
    // admin defaults explicitly and re-seed. We can't rely on the
    // render-time seed because libraryId hasn't changed.
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
    setSavedMessage(t("livetv.customize.resetDone"));
  }, [channelsQ, resetOrder, t]);

  if (livetvLibs.length === 0) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-8">
        <EmptyState
          title={t("livetv.customize.noLibraries")}
          description={t("livetv.customize.noLibrariesHint")}
        />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <header className="mb-4 flex items-start justify-between gap-3">
        <div className="flex-1">
          <Link
            to="/live-tv"
            className="inline-flex items-center gap-1 text-sm text-text-muted hover:text-text"
          >
            <ArrowLeft className="h-4 w-4" />
            {t("livetv.customize.back")}
          </Link>
          <h1 className="mt-2 text-xl font-semibold text-text">
            {t("livetv.customize.title")}
          </h1>
          <p className="text-sm text-text-muted">
            {t("livetv.customize.subtitle")}
          </p>
        </div>
      </header>

      {/* Library switcher — only render when there's more than one. */}
      {livetvLibs.length > 1 && (
        <div className="mb-3">
          <label className="block text-xs font-medium text-text-muted">
            {t("livetv.customize.library")}
          </label>
          <select
            value={libraryId}
            onChange={(e) => setSelectedLibrary(e.target.value)}
            className="mt-1 w-full rounded-[--radius-md] border border-border bg-bg-card px-3 py-2 text-sm"
          >
            {livetvLibs.map((l) => (
              <option key={l.id} value={l.id}>
                {l.name}
              </option>
            ))}
          </select>
        </div>
      )}

      <p className="mb-3 text-xs text-text-muted">
        {t("livetv.customize.scopeHint")}
      </p>

      <ChannelOrderEditor
        draft={draft}
        loading={channelsQ.isLoading}
        onMove={move}
        onToggleHidden={toggleHidden}
        onSave={handleSave}
        onReset={handleReset}
        dirty={dirty}
        savePending={replaceOrder.isPending}
        resetPending={resetOrder.isPending}
        savedMessage={savedMessage}
      />
    </div>
  );
}
