// ChannelOrderEditor — presentational list editor for channel
// reordering + per-channel visibility. Shared by the per-user
// personalisation page (/live-tv/customize) and the admin curation
// section (/admin/libraries/{id}).
//
// Affordances (v2 — replaces the one-position-at-a-time arrows):
//
//   - Drag & drop. Mouse, touch and keyboard via @dnd-kit. Focus
//     the grip handle, press Space to lift, arrows to move, Space
//     again to drop. Screen-reader announcements come for free.
//
//   - Position jump. Click the row number, type the destination,
//     press Enter. Lets the operator move a row from #340 to #5
//     without a 335-step drag.
//
//   - Search. Filter by name or group while the underlying order
//     is preserved. Drag still operates on the *real* indices, so
//     moves while filtered land correctly in the full list.
//
//   - Bulk selection. Checkboxes per row; sticky action bar shows
//     "hide", "show" and "move to…" once anything is selected.
//
//   - Sticky save bar with an unsaved-changes badge so the action
//     is never scrolled off-screen on a 500-row IPTV catalogue.
//
// Controlled component: the parent owns the draft array. The editor
// emits intent (reorder, toggle, bulk-hide) and the parent rebuilds
// the array — this keeps the save/reset mutations on the right side
// of the user-vs-admin scope boundary.

import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowDownToLine,
  ArrowUpToLine,
  Eye,
  EyeOff,
  GripVertical,
  RotateCcw,
  Save,
  Search,
  X,
} from "lucide-react";
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
  /** Move the item at `from` to index `to`. The parent applies the
   *  reorder (typically via `arrayMove`) — keeps the editor pure. */
  onReorder: (from: number, to: number) => void;
  onToggleHidden: (index: number) => void;
  /** Set hidden = `hidden` on the supplied channel ids in one go.
   *  Used by the bulk-action bar; the editor never mutates the draft
   *  directly. */
  onBulkSetHidden: (ids: string[], hidden: boolean) => void;
  onSave: () => void;
  onReset: () => void;
  dirty: boolean;
  savePending: boolean;
  resetPending: boolean;
  savedMessage: string | null;
  /** Optional one-line description rendered above the list. Used by
   *  callers to clarify scope ("only your view" vs "the default
   *  every user sees"). */
  hint?: string;
  saveLabelKey?: string;
  resetLabelKey?: string;
  emptyTitleKey?: string;
  emptyHintKey?: string;
}

export function ChannelOrderEditor({
  draft,
  loading,
  onReorder,
  onToggleHidden,
  onBulkSetHidden,
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
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [editingPos, setEditingPos] = useState<string | null>(null);

  // Pointer needs a small drag distance before activation; otherwise
  // a normal click on the row body would steal the focus from the
  // checkbox / position input.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // Filtered VIEW. The underlying order is unchanged; we just gate
  // which rows render. Drag drops carry the *real* index in the
  // payload so a move while filtered still lands in the right slot
  // of the full list.
  const trimmed = query.trim().toLowerCase();
  const visible = useMemo(() => {
    if (!trimmed) return draft.map((c, i) => ({ channel: c, realIndex: i }));
    return draft
      .map((c, i) => ({ channel: c, realIndex: i }))
      .filter(
        ({ channel }) =>
          channel.name.toLowerCase().includes(trimmed) ||
          (channel.group_name?.toLowerCase().includes(trimmed) ?? false),
      );
  }, [draft, trimmed]);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const from = draft.findIndex((c) => c.id === active.id);
      const to = draft.findIndex((c) => c.id === over.id);
      if (from === -1 || to === -1) return;
      onReorder(from, to);
    },
    [draft, onReorder],
  );

  const toggleSelect = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const clearSelection = useCallback(() => setSelected(new Set()), []);

  const selectAllVisible = useCallback(() => {
    setSelected(new Set(visible.map((v) => v.channel.id)));
  }, [visible]);

  const handleBulkHide = useCallback(
    (hidden: boolean) => {
      onBulkSetHidden(Array.from(selected), hidden);
      clearSelection();
    },
    [onBulkSetHidden, selected, clearSelection],
  );

  const handleJumpTo = useCallback(
    (realIndex: number, rawValue: string) => {
      setEditingPos(null);
      const parsed = parseInt(rawValue, 10);
      if (Number.isNaN(parsed)) return;
      // 1-based input from the user; clamp to valid range.
      const target = Math.min(Math.max(parsed, 1), draft.length) - 1;
      if (target === realIndex) return;
      onReorder(realIndex, target);
    },
    [draft.length, onReorder],
  );

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

  const visibleIds = visible.map((v) => v.channel.id);
  const allVisibleSelected =
    visible.length > 0 && visible.every((v) => selected.has(v.channel.id));
  const someSelected = selected.size > 0;
  const hiddenCount = draft.filter((c) => c.hidden).length;

  return (
    <div>
      {hint && <p className="mb-3 text-xs text-text-muted">{hint}</p>}

      {/* Toolbar: search + counters. */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[180px]">
          <Search
            className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted"
            aria-hidden="true"
          />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("livetv.customize.searchPlaceholder", {
              defaultValue: "Buscar canal o grupo…",
            })}
            aria-label={t("livetv.customize.searchPlaceholder", {
              defaultValue: "Buscar canal o grupo",
            })}
            className="w-full rounded-[--radius-md] border border-border bg-bg-card py-2 pl-8 pr-8 text-sm text-text placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
          />
          {query && (
            <button
              type="button"
              onClick={() => setQuery("")}
              aria-label={t("livetv.customize.searchClear", {
                defaultValue: "Limpiar búsqueda",
              })}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-text-muted hover:bg-bg-elevated"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <span className="text-xs text-text-muted">
          {t("livetv.customize.counts", {
            defaultValue: "{{visible}} de {{total}} · {{hidden}} ocultos",
            visible: visible.length,
            total: draft.length,
            hidden: hiddenCount,
          })}
        </span>
      </div>

      {/* Bulk-selection action bar. Renders only when at least one
          row is selected so it doesn't compete with the empty state
          on first load. */}
      {someSelected && (
        <div
          role="region"
          aria-label={t("livetv.customize.bulkAriaLabel", {
            defaultValue: "Acciones en bloque",
          })}
          className="mb-2 flex flex-wrap items-center justify-between gap-2 rounded-[--radius-md] border border-accent/30 bg-accent/10 px-3 py-2 text-sm"
        >
          <span className="font-medium text-text">
            {t("livetv.customize.bulkSelected", {
              defaultValue: "{{count}} seleccionados",
              count: selected.size,
            })}
          </span>
          <div className="flex flex-wrap items-center gap-1">
            <Button variant="ghost" size="sm" onClick={() => handleBulkHide(true)}>
              <EyeOff className="h-3.5 w-3.5" />
              {t("livetv.customize.bulkHide", { defaultValue: "Ocultar" })}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => handleBulkHide(false)}>
              <Eye className="h-3.5 w-3.5" />
              {t("livetv.customize.bulkShow", { defaultValue: "Mostrar" })}
            </Button>
            <Button variant="ghost" size="sm" onClick={clearSelection}>
              {t("livetv.customize.bulkClear", { defaultValue: "Quitar selección" })}
            </Button>
          </div>
        </div>
      )}

      {/* Sticky list header — checkbox to select all visible + column
          labels. The "#" column doubles as the position-jump trigger. */}
      <div className="mb-1 flex items-center gap-2 px-3 py-1.5 text-[11px] uppercase tracking-wide text-text-muted">
        <input
          type="checkbox"
          checked={allVisibleSelected}
          onChange={() => (allVisibleSelected ? clearSelection() : selectAllVisible())}
          aria-label={t("livetv.customize.selectAll", {
            defaultValue: "Seleccionar todo lo visible",
          })}
          className="h-3.5 w-3.5 cursor-pointer"
        />
        <span className="w-4" aria-hidden="true" />
        <span className="w-12 text-right">#</span>
        <span className="flex-1">
          {t("livetv.customize.colChannel", { defaultValue: "Canal" })}
        </span>
        <span className="text-right">
          {t("livetv.customize.colActions", { defaultValue: "Acciones" })}
        </span>
      </div>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={visibleIds} strategy={verticalListSortingStrategy}>
          <ol className="rounded-[--radius-md] border border-border bg-bg-card divide-y divide-border-subtle">
            {visible.length === 0 ? (
              <li className="px-3 py-6 text-center text-sm text-text-muted">
                {t("livetv.customize.noMatches", {
                  defaultValue: "Ningún canal coincide con la búsqueda.",
                })}
              </li>
            ) : (
              visible.map(({ channel, realIndex }) => (
                <SortableRow
                  key={channel.id}
                  channel={channel}
                  realIndex={realIndex}
                  total={draft.length}
                  selected={selected.has(channel.id)}
                  onToggleSelect={() => toggleSelect(channel.id)}
                  onToggleHidden={() => onToggleHidden(realIndex)}
                  onMoveToTop={() => onReorder(realIndex, 0)}
                  onMoveToBottom={() => onReorder(realIndex, draft.length - 1)}
                  editingPos={editingPos === channel.id}
                  onStartEditPos={() => setEditingPos(channel.id)}
                  onCancelEditPos={() => setEditingPos(null)}
                  onCommitPos={(value) => handleJumpTo(realIndex, value)}
                />
              ))
            )}
          </ol>
        </SortableContext>
      </DndContext>

      {savedMessage && (
        <div
          role="status"
          className="mt-3 rounded-[--radius-sm] bg-success/10 px-3 py-2 text-sm text-success"
        >
          ✓ {savedMessage}
        </div>
      )}

      {/* Sticky save bar — visible only while there are unsaved
          changes, so the panel stays calm at rest. */}
      <div
        className={[
          "sticky bottom-0 -mx-3 mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-border bg-bg-card/95 px-3 py-3 backdrop-blur transition-opacity",
          dirty ? "opacity-100" : "pointer-events-none opacity-0",
        ].join(" ")}
      >
        <span className="text-xs text-text-muted" aria-live="polite">
          {dirty
            ? t("livetv.customize.unsaved", { defaultValue: "Cambios sin guardar" })
            : ""}
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="ghost" onClick={onReset} disabled={resetPending}>
            <RotateCcw className="h-4 w-4" />
            {t(resetLabelKey)}
          </Button>
          <Button variant="primary" onClick={onSave} disabled={!dirty || savePending}>
            {savePending ? <Spinner size="sm" /> : <Save className="h-4 w-4" />}
            {t(saveLabelKey)}
          </Button>
        </div>
      </div>
    </div>
  );
}

interface SortableRowProps {
  channel: DraftChannel;
  realIndex: number;
  total: number;
  selected: boolean;
  onToggleSelect: () => void;
  onToggleHidden: () => void;
  onMoveToTop: () => void;
  onMoveToBottom: () => void;
  editingPos: boolean;
  onStartEditPos: () => void;
  onCancelEditPos: () => void;
  onCommitPos: (value: string) => void;
}

function SortableRow({
  channel,
  realIndex,
  total,
  selected,
  onToggleSelect,
  onToggleHidden,
  onMoveToTop,
  onMoveToBottom,
  editingPos,
  onStartEditPos,
  onCancelEditPos,
  onCommitPos,
}: SortableRowProps) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: channel.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <li
      ref={setNodeRef}
      style={style}
      className={[
        "flex items-center gap-2 px-3 py-2 text-sm transition-colors",
        channel.hidden ? "opacity-50" : "",
        isDragging ? "z-10 bg-bg-elevated shadow-lg" : "",
        selected ? "bg-accent/5" : "",
      ].join(" ")}
      data-testid="customize-row"
    >
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggleSelect}
        aria-label={t("livetv.customize.selectRow", {
          defaultValue: "Seleccionar {{name}}",
          name: channel.name,
        })}
        className="h-3.5 w-3.5 cursor-pointer"
      />

      <button
        type="button"
        {...attributes}
        {...listeners}
        aria-label={t("livetv.customize.dragHandle", {
          defaultValue: "Arrastrar para reordenar {{name}}",
          name: channel.name,
        })}
        className="cursor-grab touch-none rounded p-1 text-text-muted hover:bg-bg-elevated focus:outline-none focus:ring-1 focus:ring-accent active:cursor-grabbing"
      >
        <GripVertical className="h-4 w-4" />
      </button>

      {/* Position cell — click to edit. Input commits on Enter / blur,
          cancels on Escape. */}
      {editingPos ? (
        <input
          ref={(el) => {
            inputRef.current = el;
            if (el) {
              el.focus();
              el.select();
            }
          }}
          type="number"
          min={1}
          max={total}
          defaultValue={realIndex + 1}
          onBlur={(e) => onCommitPos(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              onCommitPos((e.target as HTMLInputElement).value);
            } else if (e.key === "Escape") {
              e.preventDefault();
              onCancelEditPos();
            }
          }}
          aria-label={t("livetv.customize.jumpTo", {
            defaultValue: "Mover a posición",
          })}
          className="w-12 rounded border border-accent bg-bg px-1 py-0.5 text-right font-mono text-xs text-text focus:outline-none"
        />
      ) : (
        <button
          type="button"
          onClick={onStartEditPos}
          aria-label={t("livetv.customize.jumpToFor", {
            defaultValue: "Cambiar posición de {{name}} (actual {{pos}})",
            name: channel.name,
            pos: realIndex + 1,
          })}
          className="w-12 rounded px-1 py-0.5 text-right font-mono text-xs text-text-muted hover:bg-bg-elevated hover:text-text"
        >
          {realIndex + 1}
        </button>
      )}

      <div className="flex flex-1 flex-col min-w-0">
        <span className="truncate text-text">{channel.name}</span>
        {channel.group_name && (
          <span className="truncate text-xs text-text-muted">{channel.group_name}</span>
        )}
      </div>

      <div className="flex items-center gap-0.5">
        <button
          type="button"
          onClick={onMoveToTop}
          disabled={realIndex === 0}
          aria-label={t("livetv.customize.moveToTop", {
            defaultValue: "Mover al principio",
          })}
          className="rounded p-1.5 text-text-muted hover:bg-bg-elevated disabled:opacity-30"
        >
          <ArrowUpToLine className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={onMoveToBottom}
          disabled={realIndex === total - 1}
          aria-label={t("livetv.customize.moveToBottom", {
            defaultValue: "Mover al final",
          })}
          className="rounded p-1.5 text-text-muted hover:bg-bg-elevated disabled:opacity-30"
        >
          <ArrowDownToLine className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={onToggleHidden}
          aria-label={
            channel.hidden
              ? t("livetv.customize.show")
              : t("livetv.customize.hide")
          }
          aria-pressed={channel.hidden}
          className={[
            "rounded p-1.5 hover:bg-bg-elevated",
            channel.hidden ? "text-danger" : "text-text-muted",
          ].join(" ")}
        >
          {channel.hidden ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
        </button>
      </div>
    </li>
  );
}

