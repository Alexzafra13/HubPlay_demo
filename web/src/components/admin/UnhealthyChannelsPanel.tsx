import { useTranslation } from "react-i18next";
import {
  useDisableChannel,
  useResetChannelHealth,
  useUnhealthyChannels,
} from "@/api/hooks";
import type { UnhealthyChannel } from "@/api/types";
import { Button } from "@/components/common";

/**
 * UnhealthyChannelsPanel — admin surface for channels the proxy has
 * recently failed on. Appears as a second row beneath livetv
 * libraries (next to the EPG sources panel), but only when there's
 * something to report — a healthy library shows nothing.
 *
 * Actions per row:
 *   - "Marcar como OK": clears the failure counter. Use when the
 *     operator knows the channel is working (tested it elsewhere) or
 *     when they've fixed the upstream issue manually.
 *   - "Desactivar": flips is_active=false. The channel disappears from
 *     every user surface until re-enabled via the main library view.
 *
 * The list polls every 30 seconds (configured in the hook) so viewer
 * activity is reflected without a manual refresh.
 */
export function UnhealthyChannelsPanel({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const { data: channels = [], isLoading } = useUnhealthyChannels(libraryId);
  const resetHealth = useResetChannelHealth(libraryId);
  const disableChannel = useDisableChannel(libraryId);

  if (isLoading || channels.length === 0) {
    return null; // Render nothing if nothing's broken — keep the admin UI calm.
  }

  return (
    <div className="border border-warning/40 rounded-lg p-4 bg-warning/5 space-y-3">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-semibold text-text-primary">
          {t("admin.health.title", {
            defaultValue: "Canales con problemas ({{count}})",
            count: channels.length,
          })}
        </h4>
        <span className="text-xs text-text-secondary">
          {t("admin.health.hiddenHint", {
            defaultValue:
              "Ocultos automáticamente de la vista de usuario tras 3 fallos consecutivos.",
          })}
        </span>
      </div>
      <ol className="space-y-2" aria-label={t("admin.health.listLabel", {
        defaultValue: "Canales con fallos recientes",
      })}>
        {channels.map((ch) => (
          <UnhealthyRow
            key={ch.id}
            channel={ch}
            onReset={() => resetHealth.mutate(ch.id)}
            onDisable={() => disableChannel.mutate(ch.id)}
            isBusy={resetHealth.isPending || disableChannel.isPending}
          />
        ))}
      </ol>
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
