import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useDisableChannel,
  useResetChannelHealth,
  useUnhealthyChannels,
} from "@/api/hooks";
import type { UnhealthyChannel } from "@/api/types";
import { Button } from "@/components/common";

/** Match ChannelsWithoutEPGPanel's pager. A library with 80+ unhealthy
 * channels (Spanish providers under court-ordered IP block routinely
 * lose dozens at once) would otherwise drop a wall of <li>s into the
 * DOM. */
const INITIAL_VISIBLE = 20;
const PAGE_INCREMENT = 40;

/**
 * UnhealthyChannelsPanel — admin surface for channels the proxy has
 * recently failed on. Appears as a tab body inside LivetvAdminPanel,
 * shown only when the library has channels in error state — a healthy
 * library shows nothing.
 *
 * Actions per row:
 *   - "Marcar como OK": clears the failure counter. Use when the
 *     operator knows the channel is working (tested it elsewhere) or
 *     when they've fixed the upstream issue manually.
 *   - "Desactivar": flips is_active=false. The channel disappears from
 *     every user surface until re-enabled via the main library view.
 *
 * Search + pagination match ChannelsWithoutEPGPanel so the two tabs
 * feel like the same surface; users with 100+ entries can find
 * specific channels without scrolling forever.
 *
 * The list polls every 30 seconds (configured in the hook) so viewer
 * activity is reflected without a manual refresh.
 */
export function UnhealthyChannelsPanel({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const { data: channels = [], isLoading } = useUnhealthyChannels(libraryId);
  const resetHealth = useResetChannelHealth(libraryId);
  const disableChannel = useDisableChannel(libraryId);

  const [search, setSearch] = useState("");
  const [visibleCount, setVisibleCount] = useState(INITIAL_VISIBLE);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return channels;
    return channels.filter(
      (ch) =>
        ch.name.toLowerCase().includes(q) ||
        (ch.last_probe_error ?? "").toLowerCase().includes(q),
    );
  }, [channels, search]);

  const visible = filtered.slice(0, visibleCount);
  const hasMore = visible.length < filtered.length;

  if (isLoading || channels.length === 0) {
    return null;
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-text-secondary">
        {t("admin.health.hiddenHint", {
          defaultValue:
            "Ocultos automáticamente de la vista de usuario tras 3 fallos consecutivos. Márcalos como OK cuando los arregles o desactívalos si siguen muertos.",
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
          placeholder={t("admin.health.searchPlaceholder", {
            defaultValue: "Buscar por nombre o motivo del fallo…",
          })}
          className="flex-1 px-3 py-2 text-sm bg-bg-card border border-border rounded"
          aria-label={t("admin.health.searchLabel", {
            defaultValue: "Filtrar canales con problemas",
          })}
        />
        <span className="text-xs text-text-secondary tabular-nums shrink-0">
          {t("admin.health.counter", {
            defaultValue: "{{shown}} de {{total}}",
            shown: Math.min(visibleCount, filtered.length),
            total: filtered.length,
          })}
        </span>
      </div>

      {filtered.length === 0 ? (
        <p className="text-sm text-text-secondary text-center py-4">
          {t("admin.health.noMatches", {
            defaultValue: "Ningún canal coincide con la búsqueda.",
          })}
        </p>
      ) : (
        <ol
          className="space-y-2"
          aria-label={t("admin.health.listLabel", {
            defaultValue: "Canales con fallos recientes",
          })}
        >
          {visible.map((ch) => (
            <UnhealthyRow
              key={ch.id}
              channel={ch}
              onReset={() => resetHealth.mutate(ch.id)}
              onDisable={() => disableChannel.mutate(ch.id)}
              isBusy={resetHealth.isPending || disableChannel.isPending}
            />
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
            {t("admin.health.showMore", {
              defaultValue: "Mostrar más ({{remaining}})",
              remaining: filtered.length - visible.length,
            })}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

interface UnhealthyRowProps {
  channel: UnhealthyChannel;
  onReset: () => void;
  onDisable: () => void;
  isBusy: boolean;
}

function UnhealthyRow({ channel, onReset, onDisable, isBusy }: UnhealthyRowProps) {
  const { t } = useTranslation();
  const lastProbe = channel.last_probe_at
    ? new Date(channel.last_probe_at).toLocaleString()
    : "—";

  return (
    <li className="flex items-center gap-2 p-2 bg-bg-card rounded border border-border/50">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-text-primary truncate">
            {channel.name}
          </span>
          <span className="text-xs text-error">
            {channel.consecutive_failures}{" "}
            {t("admin.health.failures", { defaultValue: "fallos" })}
          </span>
        </div>
        <div
          className="text-xs text-text-secondary truncate"
          title={channel.last_probe_error || undefined}
        >
          {channel.last_probe_error || t("admin.health.noError", { defaultValue: "sin detalle" })}
        </div>
        <div className="text-xs text-text-muted">
          {t("admin.health.lastProbe", {
            defaultValue: "Último intento: {{when}}",
            when: lastProbe,
          })}
        </div>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <Button
          variant="ghost"
          size="sm"
          onClick={onReset}
          disabled={isBusy}
          title={t("admin.health.resetHint", {
            defaultValue: "Borra el contador. Úsalo si sabes que el canal funciona.",
          })}
        >
          {t("admin.health.markOK", { defaultValue: "Marcar OK" })}
        </Button>
        <Button
          variant="danger"
          size="sm"
          onClick={onDisable}
          disabled={isBusy}
        >
          {t("admin.health.disable", { defaultValue: "Desactivar" })}
        </Button>
      </div>
    </li>
  );
}
