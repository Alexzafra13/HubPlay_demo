// ChannelOrderEditor — presentational list editor for channel
// reordering + per-channel visibility toggle. Shared by the
// per-user personalisation page (/live-tv/customize) and the admin
// curation section (/admin/libraries/{id}).
//
// Controlled: parent owns the draft array, the editor only emits
// move / toggle events. The parent also owns the save / reset
// mutations so each surface can target the right endpoint.
//
// Two surfaces, one component — same affordances regardless of
// where the operator drives them, matching the Sesión K.1 lesson
// (don't ship trimmed duplicates of richer UIs).

import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, Eye, EyeOff, RotateCcw, Save } from "lucide-react";
import { Button, EmptyState, Spinner } from "@/components/common";

export interface DraftChannel {
  id: string;
  name: string;
  group_name: string | null;
  hidden: boolean;
}

interface Props {
  draft: DraftChannel[];
  loading?: boolean;
  onMove: (index: number, delta: number) => void;
  onToggleHidden: (index: number) => void;
  onSave: () => void;
  onReset: () => void;
  dirty: boolean;
  savePending: boolean;
  resetPending: boolean;
  savedMessage: string | null;
  /** Optional one-line description rendered above the list. Used
   *  by callers to clarify scope ("only your view" vs "the default
   *  every user sees"). Skip when the surrounding page already
   *  carries that context. */
  hint?: string;
  /** i18n key overrides — defaults match the per-user wording. */
  saveLabelKey?: string;
  resetLabelKey?: string;
  emptyTitleKey?: string;
  emptyHintKey?: string;
}

export function ChannelOrderEditor({
  draft,
  loading,
  onMove,
  onToggleHidden,
  onSave,
  onReset,
  dirty,
  savePending,
  resetPending,
  savedMessage,
  hint,
  saveLabelKey = "livetv.customize.save",
  resetLabelKey = "livetv.customize.reset",
  emptyTitleKey = "livetv.customize.empty",
  emptyHintKey = "livetv.customize.emptyHint",
}: Props) {
  const { t } = useTranslation();

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <Spinner size="md" />
      </div>
    );
  }

  if (draft.length === 0) {
    return (
      <EmptyState
        title={t(emptyTitleKey)}
        description={t(emptyHintKey)}
      />
    );
  }

  return (
    <div>
      {hint && (
        <p className="mb-3 text-xs text-text-muted">{hint}</p>
      )}
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
                onClick={() => onMove(i, -1)}
                disabled={i === 0}
                aria-label={t("livetv.customize.moveUp")}
                className="rounded p-1.5 text-text-muted hover:bg-bg-elevated disabled:opacity-30"
              >
                <ArrowUp className="h-4 w-4" />
              </button>
              <button
                type="button"
                onClick={() => onMove(i, 1)}
                disabled={i === draft.length - 1}
                aria-label={t("livetv.customize.moveDown")}
                className="rounded p-1.5 text-text-muted hover:bg-bg-elevated disabled:opacity-30"
              >
                <ArrowDown className="h-4 w-4" />
              </button>
              <button
                type="button"
                onClick={() => onToggleHidden(i)}
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
          onClick={onReset}
          disabled={resetPending}
        >
          <RotateCcw className="h-4 w-4" />
          {t(resetLabelKey)}
        </Button>
        <Button
          variant="primary"
          onClick={onSave}
          disabled={!dirty || savePending}
        >
          {savePending ? <Spinner size="sm" /> : <Save className="h-4 w-4" />}
          {t(saveLabelKey)}
        </Button>
      </div>
    </div>
  );
}
