// DatabasePanel — admin Sistema > "Base de datos" section.
//
// Lets the operator swap the database driver (SQLite / Postgres) and
// migrate existing SQLite data into a fresh Postgres target — all
// from the web UI, no SSH, no hubplay.yaml editing.
//
// Two flows:
//
//   - "Bundled" flow (the default when docker-compose ships a `db`
//     service): the panel shows two cards, SQLite and "PostgreSQL
//     (incluido)". Picking the latter and clicking "Aplicar y
//     reiniciar" runs Test → Save → Restart in one tap. The
//     operator never sees a DSN.
//
//   - "Custom DSN" flow (collapsed by default, or always shown when
//     no bundled profile is detected): the original Test/Save form
//     for managed Postgres or other external DBs.
//
// The migration card lives below; it's shown only when the live
// driver is sqlite (no point migrating away from pg → sqlite).

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronUp, Database, RefreshCcw, ShieldAlert } from "lucide-react";

import { Badge, Button, Input, Spinner } from "@/components/common";
import { api } from "@/api/client";
import {
  useAdminDatabase,
  useAdminDatabaseProfiles,
  useRestartServer,
  useSaveAdminDatabase,
  useTestAdminDatabase,
} from "@/api/hooks";
import type {
  AdminDatabaseMigrateEvent,
  AdminDatabaseTestResponse,
  DatabaseDriver,
} from "@/api/types";

interface FormState {
  driver: DatabaseDriver;
  path: string;
  dsn: string;
}

interface MigrationState {
  running: boolean;
  table: string;
  copied: number;
  total: number;
  events: string[];
  done: boolean;
  error?: string;
}

const emptyMigration: MigrationState = {
  running: false,
  table: "",
  copied: 0,
  total: 0,
  events: [],
  done: false,
};

export function DatabasePanel() {
  const { t } = useTranslation();
  const status = useAdminDatabase();
  const profiles = useAdminDatabaseProfiles();
  const test = useTestAdminDatabase();
  const save = useSaveAdminDatabase();
  const restart = useRestartServer();

  // customOpen starts collapsed when bundled is offered (the cards
  // above are the path most operators want) and expanded otherwise
  // (operator running outside docker-compose — they need the form
  // visible or there's nothing to interact with).
  const [customOpenManual, setCustomOpenManual] = useState<boolean | null>(null);
  const hasBundledRaw = profiles.data?.bundled_postgres === true;
  const customOpen = customOpenManual ?? !hasBundledRaw;
  const [form, setForm] = useState<FormState>({
    driver: "sqlite",
    path: "",
    dsn: "",
  });
  const [testResult, setTestResult] = useState<AdminDatabaseTestResponse | null>(null);
  const [savedBanner, setSavedBanner] = useState<string | null>(null);
  const [migration, setMigration] = useState<MigrationState>(emptyMigration);
  const [oneClickBusy, setOneClickBusy] = useState<DatabaseDriver | null>(null);
  const [oneClickError, setOneClickError] = useState<string | null>(null);

  const hasBundled = hasBundledRaw;
  const liveDriver = status.data?.driver ?? "sqlite";
  const liveDSN = status.data?.dsn_redacted ?? "";
  const livePath = status.data?.path ?? "";

  // applyOneClick runs the full Test→Save→Restart sequence behind a
  // single button. Each step's failure is surfaced inline so the
  // operator knows where it bailed. Reused by both cards.
  const applyOneClick = async (target: DatabaseDriver) => {
    if (target === liveDriver) {
      setOneClickError(t("admin.database.alreadyActive"));
      return;
    }
    setOneClickBusy(target);
    setOneClickError(null);
    setSavedBanner(null);
    try {
      // Sqlite always tests fine at the conventional path; pg tests
      // against the bundled DSN. For a custom DSN target the user
      // must open the "DSN personalizado" section.
      const req =
        target === "sqlite"
          ? { driver: "sqlite" as DatabaseDriver, path: livePath || "/config/hubplay.db" }
          : { driver: "postgres" as DatabaseDriver, use_bundled: true };
      const testRes = await test.mutateAsync(req);
      if (!testRes.ok) {
        setOneClickError(testRes.error ?? t("admin.database.testFailed"));
        return;
      }
      const saveRes = await save.mutateAsync({ ...req, restart: true });
      setSavedBanner(
        saveRes.restart_scheduled
          ? t("admin.database.savedAndRestarting")
          : t("admin.database.savedNeedsRestart"),
      );
    } catch (err) {
      setOneClickError(err instanceof Error ? err.message : String(err));
    } finally {
      setOneClickBusy(null);
    }
  };

  // ─── custom DSN form (collapsible) ──────────────────────────────

  const handleTest = async () => {
    setTestResult(null);
    setSavedBanner(null);
    const res = await test.mutateAsync({
      driver: form.driver,
      path: form.driver === "sqlite" ? form.path : undefined,
      dsn: form.driver === "postgres" ? form.dsn : undefined,
    });
    setTestResult(res);
  };

  const handleSave = async (withRestart: boolean) => {
    setSavedBanner(null);
    const res = await save.mutateAsync({
      driver: form.driver,
      path: form.driver === "sqlite" ? form.path : undefined,
      dsn: form.driver === "postgres" ? form.dsn : undefined,
      restart: withRestart,
    });
    setSavedBanner(
      res.restart_scheduled
        ? t("admin.database.savedAndRestarting")
        : t("admin.database.savedNeedsRestart"),
    );
  };

  const handleRestart = async () => {
    await restart.mutateAsync();
    setSavedBanner(t("admin.database.restarting"));
  };

  const handleMigrate = async () => {
    setMigration({ ...emptyMigration, running: true });
    try {
      // Prefer the bundled DSN if available — it's the most common
      // path for a home-server operator. Custom DSN path remains as
      // a fallback (when the operator typed one in).
      const req = hasBundled
        ? { use_bundled: true, restart: false }
        : { target_dsn: form.dsn, restart: false };
      const res = await api.migrateDatabase(req);
      const reader = res.body?.getReader();
      if (!reader) {
        setMigration((m) => ({ ...m, running: false, error: "stream unavailable" }));
        return;
      }
      const decoder = new TextDecoder();
      let buf = "";
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const lines = buf.split("\n");
        buf = lines.pop() ?? "";
        for (const line of lines) {
          if (!line.trim()) continue;
          let ev: AdminDatabaseMigrateEvent;
          try {
            ev = JSON.parse(line) as AdminDatabaseMigrateEvent;
          } catch {
            continue;
          }
          setMigration((m) => applyMigrateEvent(m, ev));
        }
      }
    } catch (err) {
      setMigration((m) => ({
        ...m,
        running: false,
        error: err instanceof Error ? err.message : String(err),
      }));
    }
  };

  return (
    <section className="flex flex-col gap-4">
      <header className="flex items-start gap-3">
        <span className="rounded-[--radius-md] bg-accent/15 p-2 text-accent">
          <Database className="h-5 w-5" />
        </span>
        <div className="flex-1">
          <h3 className="text-base font-semibold text-text">
            {t("admin.database.title")}
          </h3>
          <p className="text-sm text-text-muted">
            {t("admin.database.subtitle")}
          </p>
        </div>
      </header>

      {/* ── Live status ─────────────────────────────────────────────── */}
      <div className="rounded-[--radius-md] border border-border bg-bg-elevated p-4">
        {status.isLoading ? (
          <Spinner size="sm" />
        ) : status.data ? (
          <dl className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-text-muted">{t("admin.database.activeDriver")}</dt>
              <dd className="mt-1">
                <Badge variant={liveDriver === "postgres" ? "success" : "default"}>
                  {liveDriver === "postgres" ? "PostgreSQL" : "SQLite"}
                </Badge>
              </dd>
            </div>
            <div>
              <dt className="text-text-muted">
                {liveDriver === "postgres"
                  ? t("admin.database.dsn")
                  : t("admin.database.path")}
              </dt>
              <dd className="mt-1 break-all font-mono text-xs text-text">
                {liveDriver === "postgres" ? liveDSN : livePath}
              </dd>
            </div>
            <div>
              <dt className="text-text-muted">{t("admin.database.poolUsage")}</dt>
              <dd className="mt-1 text-text">
                {status.data.pool.in_use}/{status.data.pool.max_open}{" "}
                <span className="text-text-muted">
                  ({status.data.pool.idle} {t("admin.database.poolIdle")})
                </span>
              </dd>
            </div>
            <div>
              <dt className="text-text-muted">{t("admin.database.poolWaits")}</dt>
              <dd className="mt-1 text-text">
                {status.data.pool.wait_count} (
                {status.data.pool.wait_duration_ms} ms)
              </dd>
            </div>
          </dl>
        ) : (
          <p className="text-sm text-text-muted">{t("common.error")}</p>
        )}
      </div>

      {/* ── One-click picker (only when bundled is available) ───────── */}
      {hasBundled && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <OneClickCard
            label="SQLite"
            description={t("admin.database.sqliteHint")}
            active={liveDriver === "sqlite"}
            busy={oneClickBusy === "sqlite"}
            disabled={oneClickBusy !== null}
            onPick={() => void applyOneClick("sqlite")}
            t={t}
          />
          <OneClickCard
            label="PostgreSQL"
            description={t("admin.database.bundledHint")}
            active={liveDriver === "postgres"}
            busy={oneClickBusy === "postgres"}
            disabled={oneClickBusy !== null}
            onPick={() => void applyOneClick("postgres")}
            t={t}
          />
        </div>
      )}

      {oneClickError && (
        <div role="alert" className="rounded-[--radius-sm] bg-danger/10 px-3 py-2 text-sm text-danger">
          ✗ {oneClickError}
        </div>
      )}

      {savedBanner && (
        <div role="status" className="rounded-[--radius-sm] bg-info/10 px-3 py-2 text-sm text-info">
          {savedBanner}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" onClick={handleRestart} disabled={restart.isPending}>
          <RefreshCcw className="h-4 w-4" />
          {t("admin.database.restartOnly")}
        </Button>
        <button
          type="button"
          onClick={() => setCustomOpenManual(!customOpen)}
          className="ml-auto inline-flex items-center gap-1 text-sm text-text-muted hover:text-text"
          aria-expanded={customOpen}
        >
          {customOpen ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
          {t("admin.database.customDSN")}
        </button>
      </div>

      {/* ── Custom DSN form (collapsible) ───────────────────────────── */}
      {customOpen && (
        <div className="rounded-[--radius-md] border border-border bg-bg-card p-4">
          <p className="mb-3 text-xs text-text-muted">
            {t("admin.database.customDSNHint")}
          </p>

          <fieldset className="mb-3 flex flex-wrap gap-2">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="db-driver"
                value="sqlite"
                checked={form.driver === "sqlite"}
                onChange={() => setForm((f) => ({ ...f, driver: "sqlite" }))}
              />
              SQLite
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="db-driver"
                value="postgres"
                checked={form.driver === "postgres"}
                onChange={() => setForm((f) => ({ ...f, driver: "postgres" }))}
              />
              PostgreSQL
            </label>
          </fieldset>

          {form.driver === "sqlite" ? (
            <Input
              label={t("admin.database.path")}
              value={form.path}
              onChange={(e) => setForm((f) => ({ ...f, path: e.target.value }))}
              placeholder="/config/hubplay.db"
              spellCheck={false}
            />
          ) : (
            <Input
              label={t("admin.database.dsn")}
              value={form.dsn}
              onChange={(e) => setForm((f) => ({ ...f, dsn: e.target.value }))}
              placeholder="postgres://user:pass@host:5432/hubplay?sslmode=require"
              spellCheck={false}
              type="password"
            />
          )}

          <div className="mt-3 flex flex-wrap gap-2">
            <Button variant="secondary" onClick={handleTest} disabled={test.isPending}>
              {test.isPending ? <Spinner size="sm" /> : null}
              {t("admin.database.test")}
            </Button>
            <Button
              variant="secondary"
              onClick={() => void handleSave(false)}
              disabled={save.isPending || !testResult?.ok}
            >
              {t("admin.database.save")}
            </Button>
            <Button
              variant="primary"
              onClick={() => void handleSave(true)}
              disabled={save.isPending || !testResult?.ok}
            >
              {t("admin.database.saveAndRestart")}
            </Button>
          </div>

          {testResult && (
            <div
              role="status"
              className={[
                "mt-3 rounded-[--radius-sm] px-3 py-2 text-sm",
                testResult.ok ? "bg-success/10 text-success" : "bg-danger/10 text-danger",
              ].join(" ")}
            >
              {testResult.ok ? (
                <span>
                  ✓ {t("admin.database.testOK")}
                  {testResult.server_version ? ` — ${testResult.server_version}` : ""}{" "}
                  <span className="text-text-muted">({testResult.duration_ms} ms)</span>
                </span>
              ) : (
                <span>✗ {testResult.error}</span>
              )}
            </div>
          )}
        </div>
      )}

      {/* ── Data migration (only when live driver is sqlite) ────────── */}
      {liveDriver === "sqlite" && (
        <div className="rounded-[--radius-md] border border-warning/30 bg-warning/5 p-4">
          <header className="mb-3 flex items-start gap-2">
            <ShieldAlert className="mt-0.5 h-5 w-5 text-warning" />
            <div className="flex-1">
              <h4 className="text-sm font-semibold text-text">
                {t("admin.database.migrateTitle")}
              </h4>
              <p className="text-sm text-text-muted">
                {t("admin.database.migrateSubtitle")}
              </p>
            </div>
          </header>

          <p className="mb-3 text-xs text-text-muted">
            {hasBundled
              ? t("admin.database.migrateHintBundled")
              : t("admin.database.migrateHint")}
          </p>

          {!hasBundled && (
            <Input
              label={t("admin.database.migrateTargetDSN")}
              value={form.driver === "postgres" ? form.dsn : ""}
              onChange={(e) =>
                setForm({ driver: "postgres", path: "", dsn: e.target.value })
              }
              placeholder="postgres://user:pass@host:5432/hubplay?sslmode=require"
              spellCheck={false}
              type="password"
            />
          )}

          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              variant="primary"
              onClick={handleMigrate}
              disabled={migration.running || (!hasBundled && !form.dsn)}
            >
              {migration.running ? <Spinner size="sm" /> : null}
              {t("admin.database.migrateStart")}
            </Button>
          </div>

          {(migration.running || migration.done || migration.error) && (
            <div className="mt-4 space-y-2 text-sm">
              {migration.running && (
                <p className="text-text">
                  {t("admin.database.migrateInProgress")}{" "}
                  <span className="font-mono text-xs">
                    {migration.table}{" "}
                    {migration.total > 0
                      ? `${migration.copied}/${migration.total}`
                      : ""}
                  </span>
                </p>
              )}
              {migration.error && <p className="text-danger">✗ {migration.error}</p>}
              {migration.done && !migration.error && (
                <p className="text-success">✓ {t("admin.database.migrateDone")}</p>
              )}
              {migration.events.length > 0 && (
                <details className="text-xs text-text-muted">
                  <summary className="cursor-pointer">
                    {t("admin.database.migrateLog")}
                  </summary>
                  <pre className="mt-2 max-h-40 overflow-auto rounded bg-bg-base p-2 font-mono">
                    {migration.events.join("\n")}
                  </pre>
                </details>
              )}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

// OneClickCard is a radio-like card the operator clicks to apply a
// preset driver. Visual cue: active driver gets the accent border
// and a "actual" badge so picking the same one again is obviously a
// no-op.
function OneClickCard({
  label,
  description,
  active,
  busy,
  disabled,
  onPick,
  t,
}: {
  label: string;
  description: string;
  active: boolean;
  busy: boolean;
  disabled: boolean;
  onPick: () => void;
  t: (key: string) => string;
}) {
  return (
    <button
      type="button"
      onClick={onPick}
      disabled={disabled || active}
      aria-pressed={active}
      className={[
        "flex flex-col items-start gap-1 rounded-[--radius-md] border p-4 text-left transition-colors",
        active
          ? "border-accent bg-accent/5"
          : "border-border bg-bg-card hover:border-accent/50 disabled:opacity-50",
      ].join(" ")}
    >
      <div className="flex w-full items-center justify-between">
        <span className="font-medium text-text">{label}</span>
        {active && <Badge variant="success">{t("admin.database.activeNow")}</Badge>}
        {busy && <Spinner size="sm" />}
      </div>
      <p className="text-xs text-text-muted">{description}</p>
    </button>
  );
}

function applyMigrateEvent(
  state: MigrationState,
  ev: AdminDatabaseMigrateEvent,
): MigrationState {
  const log = (msg: string): string[] => {
    const next = [...state.events, msg];
    return next.length > 200 ? next.slice(next.length - 200) : next;
  };
  switch (ev.event) {
    case "start":
      return { ...state, events: log(`start ${ev.source}→${ev.target}`) };
    case "progress":
      return {
        ...state,
        table: ev.table,
        copied: ev.copied,
        total: ev.total,
        events: log(`${ev.phase} ${ev.table} ${ev.copied}/${ev.total}`),
      };
    case "warning":
      return { ...state, events: log(`warn: ${ev.message}`) };
    case "config_saved":
      return { ...state, events: log("config saved") };
    case "restart_scheduled":
      return { ...state, events: log("restart scheduled") };
    case "error":
      return { ...state, running: false, error: ev.message, events: log(`error: ${ev.message}`) };
    case "done":
      return {
        ...state,
        running: false,
        done: true,
        copied: ev.rows_copied,
        total: ev.rows_copied,
        events: log(
          `done — ${ev.tables_copied} tables, ${ev.rows_copied} rows, ${ev.duration_ms} ms`,
        ),
      };
    default:
      return state;
  }
}
