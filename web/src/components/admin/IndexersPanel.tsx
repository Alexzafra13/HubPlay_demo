import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, XCircle, Trash2, Plus, Loader2 } from "lucide-react";

import { api } from "@/api/client";
import { ApiError, type IndexerInput, type IndexerStatus } from "@/api/types";
import { Button, Input, Spinner, EmptyState } from "@/components/common";

// IndexersPanel — plug-and-play management of Torznab/Prowlarr indexers.
// Everything is persisted server-side (DB), so the operator never edits
// yaml/env: add a Prowlarr base URL + API key here, test it, save, done.
export function IndexersPanel() {
  const { t } = useTranslation();
  const qc = useQueryClient();

  const list = useQuery({
    queryKey: ["admin-indexers"],
    queryFn: () => api.listIndexers(),
    retry: false,
  });

  const invalidate = () => qc.invalidateQueries({ queryKey: ["admin-indexers"] });

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteIndexer(id),
    onSuccess: invalidate,
  });

  const toggle = useMutation({
    mutationFn: (ix: IndexerStatus) =>
      api.updateIndexer(ix.id, {
        name: ix.name,
        base_url: ix.base_url,
        url: ix.url,
        // api_key omitted → server keeps the stored secret.
        enabled: !ix.enabled,
        movie_categories: ix.movie_categories,
        series_categories: ix.series_categories,
        trackers: ix.trackers,
      }),
    onSuccess: invalidate,
  });

  // Feature unavailable on this server (route not mounted).
  const disabled =
    list.error instanceof ApiError &&
    (list.error.status === 404 || list.error.code === "INDEXER_DISABLED");

  const indexers = list.data ?? [];

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border bg-bg-card p-5">
      <div>
        <h2 className="text-lg font-semibold text-text-primary">
          {t("indexersAdmin.title", { defaultValue: "Indexadores (Torznab / Prowlarr)" })}
        </h2>
        <p className="mt-1 text-[13px] text-text-secondary">
          {t("indexersAdmin.subtitle", {
            defaultValue:
              "Conecta tu Prowlarr/Jackett. Se guarda en el servidor — no hace falta tocar ficheros ni reiniciar.",
          })}
        </p>
      </div>

      {disabled ? (
        <EmptyState
          title={t("indexersAdmin.disabledTitle", { defaultValue: "No disponible" })}
          description={t("indexersAdmin.disabledDesc", {
            defaultValue: "La gestión de indexadores no está habilitada en este servidor.",
          })}
          icon={<XCircle strokeWidth={1.5} />}
        />
      ) : list.isLoading ? (
        <div className="flex justify-center py-8">
          <Spinner size="md" />
        </div>
      ) : (
        <>
          {indexers.length === 0 ? (
            <p className="text-sm text-text-muted">
              {t("indexersAdmin.empty", {
                defaultValue: "Aún no has añadido ningún indexador.",
              })}
            </p>
          ) : (
            <ul className="flex flex-col gap-2">
              {indexers.map((ix) => (
                <li
                  key={ix.id}
                  className="flex items-center gap-3 rounded-lg border border-border bg-bg-base/40 px-3 py-2.5"
                >
                  <span aria-hidden>
                    {ix.enabled && ix.reachable ? (
                      <CheckCircle2 className="size-4 text-success" />
                    ) : (
                      <XCircle className="size-4 text-text-muted" />
                    )}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-medium text-text-primary">
                      {ix.name}
                    </p>
                    <p className="truncate text-[11px] text-text-muted" title={ix.base_url || ix.url}>
                      {ix.base_url || ix.url}
                      {ix.trackers && ix.trackers.length > 0
                        ? ` · ${t("indexersAdmin.trackerCount", {
                            defaultValue: "{{count}} trackers",
                            count: ix.trackers.length,
                          })}`
                        : ""}
                      {ix.enabled && !ix.reachable && ix.error
                        ? ` · ${ix.error}`
                        : ""}
                    </p>
                  </div>
                  <label className="flex items-center gap-1.5 text-[11px] text-text-muted">
                    <input
                      type="checkbox"
                      checked={ix.enabled}
                      onChange={() => toggle.mutate(ix)}
                      disabled={toggle.isPending}
                    />
                    {t("indexersAdmin.enabled", { defaultValue: "Activo" })}
                  </label>
                  <button
                    type="button"
                    onClick={() => remove.mutate(ix.id)}
                    disabled={remove.isPending}
                    aria-label={t("common.delete", { defaultValue: "Eliminar" })}
                    className="flex size-8 shrink-0 items-center justify-center rounded-full text-text-muted transition-colors hover:bg-error/10 hover:text-error"
                  >
                    <Trash2 className="size-4" />
                  </button>
                </li>
              ))}
            </ul>
          )}

          <AddIndexerForm onAdded={invalidate} />
        </>
      )}
    </section>
  );
}

function AddIndexerForm({ onAdded }: { onAdded: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [testMsg, setTestMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const create = useMutation({
    mutationFn: (input: IndexerInput) => api.createIndexer(input),
    onSuccess: () => {
      setName("");
      setBaseURL("");
      setApiKey("");
      setTestMsg(null);
      onAdded();
    },
  });

  const test = useMutation({
    mutationFn: () => api.testIndexer({ base_url: baseURL.trim(), api_key: apiKey.trim() }),
    onSuccess: () =>
      setTestMsg({ ok: true, text: t("indexersAdmin.testOk", { defaultValue: "Conexión correcta" }) }),
    onError: (e) =>
      setTestMsg({
        ok: false,
        text: e instanceof ApiError ? e.message : t("indexersAdmin.testFail", { defaultValue: "Falló la conexión" }),
      }),
  });

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    create.mutate({ name: name.trim(), base_url: baseURL.trim(), api_key: apiKey.trim(), enabled: true });
  }

  const canSubmit = name.trim() !== "" && baseURL.trim() !== "";

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-3 border-t border-border pt-4">
      <h3 className="text-[11px] font-semibold uppercase tracking-widest text-text-muted">
        {t("indexersAdmin.addTitle", { defaultValue: "Añadir Prowlarr / Jackett" })}
      </h3>
      <div className="grid gap-2 sm:grid-cols-3">
        <Input
          label={t("indexersAdmin.name", { defaultValue: "Nombre" })}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="prowlarr"
        />
        <Input
          label={t("indexersAdmin.baseUrl", { defaultValue: "URL base" })}
          value={baseURL}
          onChange={(e) => setBaseURL(e.target.value)}
          placeholder="http://localhost:9696"
        />
        <Input
          label={t("indexersAdmin.apiKey", { defaultValue: "API key" })}
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder="••••••••"
        />
      </div>

      {testMsg && (
        <p className={`text-xs ${testMsg.ok ? "text-success" : "text-error"}`}>{testMsg.text}</p>
      )}
      {create.error && (
        <p className="text-xs text-error">
          {create.error instanceof ApiError ? create.error.message : String(create.error)}
        </p>
      )}

      <div className="flex gap-2">
        <Button
          type="button"
          variant="secondary"
          size="sm"
          disabled={baseURL.trim() === "" || test.isPending}
          onClick={() => test.mutate()}
        >
          {test.isPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
          {t("indexersAdmin.test", { defaultValue: "Probar conexión" })}
        </Button>
        <Button type="submit" size="sm" disabled={!canSubmit || create.isPending}>
          <Plus className="size-3.5" />
          {t("indexersAdmin.add", { defaultValue: "Añadir" })}
        </Button>
      </div>
    </form>
  );
}
