import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useChannelsWithoutEPG,
  usePatchChannel,
} from "@/api/hooks";
import type { ChannelWithoutEPG } from "@/api/types";
import { Button } from "@/components/common";

/**
 * ChannelsWithoutEPGPanel — shows every active channel in the library
 * that no EPG source managed to match. The admin can hand-fix the
 * tvg_id inline; the override layer on the backend makes the edit
 * survive future M3U refreshes.
 *
 * Hidden when the library has nothing to report — admins browsing a
 * healthy library see no chrome at all.
 */
export function ChannelsWithoutEPGPanel({ libraryId }: { libraryId: string }) {
  const { t } = useTranslation();
  const { data: channels = [], isLoading } = useChannelsWithoutEPG(libraryId);

  if (isLoading || channels.length === 0) return null;

  return (
    <div className="border border-border rounded-lg p-4 bg-bg-elevated/50 space-y-3">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-semibold text-text-primary">
          {t("admin.withoutEPG.title", {
            defaultValue: "Canales sin guía ({{count}})",
            count: channels.length,
          })}
        </h4>
        <span className="text-xs text-text-secondary">
          {t("admin.withoutEPG.hint", {
            defaultValue:
              "Corrige el tvg-id para emparejarlo con una entrada del XMLTV. La edición se conserva tras refrescar el M3U.",
          })}
        </span>
      </div>
      <ol
        className="space-y-2"
        aria-label={t("admin.withoutEPG.listLabel", {
          defaultValue: "Canales sin guía EPG",
        })}
      >
        {channels.map((ch) => (
          <OrphanRow key={ch.id} channel={ch} libraryId={libraryId} />
        ))}
      </ol>
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
