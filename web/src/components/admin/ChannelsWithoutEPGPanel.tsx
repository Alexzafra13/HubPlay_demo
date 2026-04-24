import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useChannelsWithoutEPG,
  usePatchChannel,
} from "@/api/hooks";
import type { ChannelWithoutEPG } from "@/api/types";
import { Button } from "@/components/common";

/** How many rows we render on first expand. A library with 250 orphans
 * would otherwise drop 250 <li>s into the DOM up front — fine for
 * performance, terrible for the admin scrolling past them every day. */
const INITIAL_VISIBLE = 20;

/** How many more rows the "Mostrar más" button appends on each click. */
const PAGE_INCREMENT = 40;

/**
 * ChannelsWithoutEPGPanel — lists every active channel in the library
 * that no EPG source managed to match. Inline editor sets `tvg_id`;
 * the override layer on the backend makes the edit survive future M3U
 * refreshes.
 *
 * Renders as tab body inside LivetvAdminPanel. The outer
 * collapse-by-default wrapper lives there (the tab itself IS the
 * collapse — clicking the tab switches to this surface). Search +
 * pagination controls remain here because they're the long-tail
 * navigation for libraries with hundreds of orphans.
 *
 * Hidden by the parent when the list is empty.
 */
export function ChannelsWithoutEPGPanel({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const { data: channels = [], isLoading } = useChannelsWithoutEPG(libraryId);

  const [search, setSearch] = useState("");
  const [visibleCount, setVisibleCount] = useState(INITIAL_VISIBLE);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return channels;
    return channels.filter((ch) =>
      ch.name.toLowerCase().includes(q) ||
      String(ch.number).includes(q) ||
      ch.tvg_id.toLowerCase().includes(q) ||
      ch.group_name.toLowerCase().includes(q),
    );
  }, [channels, search]);

  const visible = filtered.slice(0, visibleCount);
  const hasMore = visible.length < filtered.length;

  if (isLoading || channels.length === 0) return null;

  return (
    <div className="space-y-3">
      <p className="text-xs text-text-secondary">
        {t("admin.withoutEPG.hint", {
          defaultValue:
            "Corrige el tvg-id para emparejarlo con una entrada del XMLTV. La edición se conserva tras refrescar el M3U.",
        })}
      </p>
      <div className="flex items-center gap-2">
        <input
          type="search"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setVisibleCount(INITIAL_VISIBLE);
          }}
          placeholder={t("admin.withoutEPG.searchPlaceholder", {
            defaultValue: "Buscar por número, nombre o grupo…",
          })}
          className="flex-1 px-3 py-2 text-sm bg-bg-card border border-border rounded"
          aria-label={t("admin.withoutEPG.searchLabel", {
            defaultValue: "Filtrar canales sin guía",
          })}
        />
        <span className="text-xs text-text-secondary tabular-nums shrink-0">
          {t("admin.withoutEPG.counter", {
            defaultValue: "{{shown}} de {{total}}",
            shown: Math.min(visibleCount, filtered.length),
            total: filtered.length,
          })}
        </span>
      </div>

      {filtered.length === 0 ? (
        <p className="text-sm text-text-secondary text-center py-4">
          {t("admin.withoutEPG.noMatches", {
            defaultValue: "Ningún canal coincide con la búsqueda.",
          })}
        </p>
      ) : (
        <ol
          className="space-y-2"
          aria-label={t("admin.withoutEPG.listLabel", {
            defaultValue: "Canales sin guía EPG",
          })}
        >
          {visible.map((ch) => (
            <OrphanRow key={ch.id} channel={ch} libraryId={libraryId} />
          ))}
        </ol>
      )}

      {hasMore ? (
        <div className="flex justify-center pt-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setVisibleCount((n) => n + PAGE_INCREMENT)}
          >
            {t("admin.withoutEPG.showMore", {
              defaultValue: "Mostrar más ({{remaining}})",
              remaining: filtered.length - visible.length,
            })}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function OrphanRow({
  channel,
  libraryId,
}: {
  channel: ChannelWithoutEPG;
  libraryId: string;
}) {
  const { t } = useTranslation();
  const patchChannel = usePatchChannel(libraryId);

  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(channel.tvg_id);
  const [error, setError] = useState("");

  async function handleSave() {
    setError("");
    try {
      await patchChannel.mutateAsync({
        channelId: channel.id,
        patch: { tvg_id: value },
      });
      setEditing(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  function handleCancel() {
    setValue(channel.tvg_id);
    setError("");
    setEditing(false);
  }

  return (
    <li className="flex items-center gap-3 p-2 bg-bg-card rounded border border-border/50">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-text-primary truncate">
            #{channel.number} · {channel.name}
          </span>
          {channel.group_name ? (
            <span className="text-xs text-text-muted">
              {channel.group_name}
            </span>
          ) : null}
        </div>
        {editing ? (
          <div className="flex items-center gap-2 mt-1">
            <input
              type="text"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={t("admin.withoutEPG.tvgIDPlaceholder", {
                defaultValue: "tvg-id (p. ej. La1.ES)",
              })}
              className="flex-1 px-2 py-1 text-xs bg-bg-elevated border border-border rounded font-mono"
              aria-label={t("admin.withoutEPG.tvgIDLabel", {
                defaultValue: "Nuevo tvg-id",
              })}
              autoFocus
            />
            <Button
              size="sm"
              onClick={handleSave}
              isLoading={patchChannel.isPending}
            >
              {t("common.save", { defaultValue: "Guardar" })}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={handleCancel}
              disabled={patchChannel.isPending}
            >
              {t("common.cancel", { defaultValue: "Cancelar" })}
            </Button>
          </div>
        ) : (
          <div className="flex items-center gap-2 text-xs text-text-secondary">
            <span className="font-mono">
              tvg-id:{" "}
              {channel.tvg_id || (
                <em className="text-text-muted">
                  {t("admin.withoutEPG.empty", { defaultValue: "(vacío)" })}
                </em>
              )}
            </span>
          </div>
        )}
        {error ? (
          <p className="text-xs text-error mt-1" role="alert">
            {error}
          </p>
        ) : null}
      </div>
      {!editing ? (
        <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
          {t("admin.withoutEPG.edit", { defaultValue: "Editar tvg-id" })}
        </Button>
      ) : null}
    </li>
  );
}
