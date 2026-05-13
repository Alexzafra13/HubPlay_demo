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

import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { ArrowDown, ArrowUp, ArrowLeft, Eye, EyeOff, RotateCcw, Save } from "lucide-react";

import {
  useChannelsForPersonalisation,
  useLibraries,
  useReplaceChannelOrder,
  useResetChannelOrder,
} from "@/api/hooks";
import { Button, Spinner, EmptyState } from "@/components/common";

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

  // The first library auto-selected once the list resolves. Switching
  // resets the draft (we don't merge edits across libraries).
  const [libraryId, setLibraryId] = useState<string>("");
  useEffect(() => {
    if (!libraryId && livetvLibs.length > 0) {
      setLibraryId(livetvLibs[0].id);
    }
  }, [libraryId, livetvLibs]);

  const channelsQ = useChannelsForPersonalisation(libraryId || undefined);
  const replaceOrder = useReplaceChannelOrder();
  const resetOrder = useResetChannelOrder();

  const [draft, setDraft] = useState<DraftChannel[]>([]);
  const [dirty, setDirty] = useState(false);
  const [savedMessage, setSavedMessage] = useState<string | null>(null);

  // Seed the draft when the channels query resolves. We only seed
  // once per libraryId — subsequent re-fetches (e.g. after Save)
  // restart the cycle through the query invalidation in the hook.
  useEffect(() => {
    if (!channelsQ.data) return;
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
  }, [channelsQ.data]);

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
    setSavedMessage(t("livetv.customize.resetDone"));
    // The query invalidation in the hook will refetch and re-seed.
  }, [resetOrder, t]);

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
            onChange={(e) => setLibraryId(e.target.value)}
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

      {channelsQ.isLoading && (
        <div className="flex justify-center py-12">
          <Spinner size="md" />
        </div>
      )}

      {!channelsQ.isLoading && draft.length === 0 && (
        <EmptyState
          title={t("livetv.customize.empty")}
          description={t("livetv.customize.emptyHint")}
        />
      )}

      {draft.length > 0 && (
        <>
          <ol className="rounded-[--radius-md] border border-border bg-bg-card divide-y divide-border-subtle">
            {draft.map((c, i) => (
              <li
                key={c.id}
                className={[
                  "flex items-center gap-3 px-3 py-2 text-sm transition-colors",
                  c.hidden ? "opacity-50" : "",
                ].join(" ")}
                data-testid="customize-row"
              >
                <span className="w-8 text-right font-mono text-xs text-text-muted">
                  {i + 1}
                </span>
                <div className="flex flex-1 flex-col">
                  <span className="text-text">{c.name}</span>
                  {c.group_name && (
                    <span className="text-xs text-text-muted">{c.group_name}</span>
                  )}
                </div>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={() => move(i, -1)}
                    disabled={i === 0}
                    aria-label={t("livetv.customize.moveUp")}
                    className="rounded p-1.5 text-text-muted hover:bg-bg-elevated disabled:opacity-30"
                  >
                    <ArrowUp className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => move(i, 1)}
                    disabled={i === draft.length - 1}
                    aria-label={t("livetv.customize.moveDown")}
                    className="rounded p-1.5 text-text-muted hover:bg-bg-elevated disabled:opacity-30"
                  >
                    <ArrowDown className="h-4 w-4" />
                  </button>
                  <button
                    type="button"
                    onClick={() => toggleHidden(i)}
                    aria-label={c.hidden ? t("livetv.customize.show") : t("livetv.customize.hide")}
                    aria-pressed={c.hidden}
                    className={[
                      "rounded p-1.5 hover:bg-bg-elevated",
                      c.hidden ? "text-danger" : "text-text-muted",
                    ].join(" ")}
                  >
                    {c.hidden ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </li>
            ))}
          </ol>

          {savedMessage && (
            <div
              role="status"
              className="mt-3 rounded-[--radius-sm] bg-success/10 px-3 py-2 text-sm text-success"
            >
              ✓ {savedMessage}
            </div>
          )}

          <div className="mt-4 flex flex-wrap justify-between gap-2">
            <Button
              variant="ghost"
              onClick={handleReset}
              disabled={resetOrder.isPending}
            >
              <RotateCcw className="h-4 w-4" />
              {t("livetv.customize.reset")}
            </Button>
            <Button
              variant="primary"
              onClick={handleSave}
              disabled={!dirty || replaceOrder.isPending}
            >
              {replaceOrder.isPending ? <Spinner size="sm" /> : <Save className="h-4 w-4" />}
              {t("livetv.customize.save")}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
