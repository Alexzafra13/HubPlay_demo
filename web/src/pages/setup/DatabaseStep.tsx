// DatabaseStep — wizard step 0.
//
// Two flows mirroring DatabasePanel:
//
//   - Bundled: if the binary detects HUBPLAY_POSTGRES_BUNDLED_DSN
//     (i.e. running under docker-compose with the `db` service), the
//     step renders two cards — SQLite and PostgreSQL (incluido) —
//     and one click applies the choice (Test → Save → Restart).
//     The operator never sees a DSN.
//
//   - Custom DSN: when no bundled profile is present, the step falls
//     back to the manual form with a DSN/path field, same as before.
//
// Skip is still a first-class affordance — picking SQLite is the
// default and a single click moves on without any restart, since the
// binary already runs on SQLite by default.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Database } from "lucide-react";

import { api } from "@/api/client";
import type {
  AdminDatabaseProfiles,
  AdminDatabaseTestResponse,
  DatabaseDriver,
} from "@/api/types";
import { Badge, Button, Input, Spinner } from "@/components/common";

interface DatabaseStepProps {
  onNext: () => void;
}

export default function DatabaseStep({ onNext }: DatabaseStepProps) {
  const { t } = useTranslation();

  const profiles = useQuery<AdminDatabaseProfiles>({
    queryKey: ["setup", "db", "profiles"],
    queryFn: () => api.getSetupDatabaseProfiles(),
    staleTime: 60 * 60 * 1000,
  });
  const hasBundled = profiles.data?.bundled_postgres === true;

  // Bundled flow state.
  const [oneClickBusy, setOneClickBusy] = useState<DatabaseDriver | null>(null);
  const [oneClickError, setOneClickError] = useState<string | null>(null);
  const [savedBanner, setSavedBanner] = useState<string | null>(null);

  // Custom DSN form state.
  const [driver, setDriver] = useState<DatabaseDriver>("sqlite");
  const [dsn, setDsn] = useState("");
  const [path, setPath] = useState("");
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<AdminDatabaseTestResponse | null>(null);

  const applyOneClick = async (target: DatabaseDriver) => {
    setOneClickBusy(target);
    setOneClickError(null);
    setSavedBanner(null);
    try {
      if (target === "sqlite") {
        // SQLite is the default — no save/restart needed, just move on.
        onNext();
        return;
      }
      const testRes = await api.testSetupDatabase({
        driver: "postgres",
        use_bundled: true,
      });
      if (!testRes.ok) {
        setOneClickError(testRes.error ?? t("setup.database.testFailed"));
        return;
      }
      await api.saveSetupDatabase({
        driver: "postgres",
        use_bundled: true,
        restart: true,
      });
      setSavedBanner(t("setup.database.savedAndRestarting"));
    } catch (err) {
      setOneClickError(err instanceof Error ? err.message : String(err));
    } finally {
      setOneClickBusy(null);
    }
  };

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    setSavedBanner(null);
    try {
      const res = await api.testSetupDatabase({
        driver,
        path: driver === "sqlite" ? path : undefined,
        dsn: driver === "postgres" ? dsn : undefined,
      });
      setTestResult(res);
    } catch (err) {
      setTestResult({
        ok: false,
        duration_ms: 0,
        error: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setTesting(false);
    }
  };

  const handleSaveAndRestart = async () => {
    setSaving(true);
    setSavedBanner(null);
    try {
      await api.saveSetupDatabase({
        driver,
        path: driver === "sqlite" ? path : undefined,
        dsn: driver === "postgres" ? dsn : undefined,
        restart: true,
      });
      setSavedBanner(t("setup.database.savedAndRestarting"));
    } catch (err) {
      setTestResult({
        ok: false,
        duration_ms: 0,
        error: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <header className="flex items-start gap-3">
        <span className="rounded-[--radius-md] bg-accent/15 p-2 text-accent">
          <Database className="size-6" />
        </span>
        <div>
          <h2 className="text-lg font-semibold text-text">
            {t("setup.database.title")}
          </h2>
          <p className="text-sm text-text-muted">
            {t("setup.database.subtitle")}
          </p>
        </div>
      </header>

      {/* Bundled flow: one-click cards */}
      {hasBundled ? (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <ChoiceCard
              label="SQLite"
              recommended={false}
              description={t("setup.database.sqliteHint")}
              busy={oneClickBusy === "sqlite"}
              disabled={oneClickBusy !== null}
              onPick={() => void applyOneClick("sqlite")}
              t={t}
            />
            <ChoiceCard
              label="PostgreSQL"
              recommended
              description={t("setup.database.bundledHint")}
              busy={oneClickBusy === "postgres"}
              disabled={oneClickBusy !== null}
              onPick={() => void applyOneClick("postgres")}
              t={t}
            />
          </div>
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
          <p className="text-center text-xs text-text-muted">
            {t("setup.database.bundledFooter")}
          </p>
        </>
      ) : (
        // ── Fallback: custom DSN form (operator running outside docker-compose
        // or with a managed external DB).
        <>
          <fieldset className="flex flex-wrap gap-4">
            <label className="flex flex-1 min-w-[200px] cursor-pointer items-start gap-3 rounded-[--radius-md] border border-border bg-bg-elevated p-3 hover:border-accent/50">
              <input
                type="radio"
                name="setup-db-driver"
                checked={driver === "sqlite"}
                onChange={() => setDriver("sqlite")}
                className="mt-1"
              />
              <div>
                <span className="font-medium text-text">SQLite</span>
                <p className="text-xs text-text-muted">
                  {t("setup.database.sqliteHint")}
                </p>
              </div>
            </label>
            <label className="flex flex-1 min-w-[200px] cursor-pointer items-start gap-3 rounded-[--radius-md] border border-border bg-bg-elevated p-3 hover:border-accent/50">
              <input
                type="radio"
                name="setup-db-driver"
                checked={driver === "postgres"}
                onChange={() => setDriver("postgres")}
                className="mt-1"
              />
              <div>
                <span className="font-medium text-text">PostgreSQL</span>
                <p className="text-xs text-text-muted">
                  {t("setup.database.postgresHint")}
                </p>
              </div>
            </label>
          </fieldset>

          {driver === "sqlite" && (
            <Input
              label={t("setup.database.path")}
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/config/hubplay.db"
              spellCheck={false}
              hint={t("setup.database.pathHint")}
            />
          )}

          {driver === "postgres" && (
            <Input
              label={t("setup.database.dsn")}
              value={dsn}
              onChange={(e) => setDsn(e.target.value)}
              placeholder="postgres://user:pass@host:5432/hubplay?sslmode=require"
              spellCheck={false}
              type="password"
              hint={t("setup.database.dsnHint")}
            />
          )}

          {testResult && (
            <div
              role="status"
              className={[
                "rounded-[--radius-sm] px-3 py-2 text-sm",
                testResult.ok ? "bg-success/10 text-success" : "bg-danger/10 text-danger",
              ].join(" ")}
            >
              {testResult.ok ? (
                <span>
                  ✓ {t("setup.database.testOK")}
                  {testResult.server_version ? ` — ${testResult.server_version}` : ""}{" "}
                  <span className="text-text-muted">({testResult.duration_ms} ms)</span>
                </span>
              ) : (
                <span>✗ {testResult.error}</span>
              )}
            </div>
          )}

          {savedBanner && (
            <div role="status" className="rounded-[--radius-sm] bg-info/10 px-3 py-2 text-sm text-info">
              {savedBanner}
            </div>
          )}

          <div className="flex flex-wrap justify-between gap-2 pt-2">
            <Button variant="ghost" onClick={onNext} disabled={saving}>
              {t("setup.database.skip")}
            </Button>
            <div className="flex gap-2">
              <Button
                variant="secondary"
                onClick={handleTest}
                disabled={testing || saving || (driver === "sqlite" ? !path : !dsn)}
              >
                {testing ? <Spinner size="sm" /> : null}
                {t("setup.database.test")}
              </Button>
              <Button
                variant="primary"
                onClick={handleSaveAndRestart}
                disabled={!testResult?.ok || saving}
              >
                {saving ? <Spinner size="sm" /> : null}
                {t("setup.database.saveAndRestart")}
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function ChoiceCard({
  label,
  recommended,
  description,
  busy,
  disabled,
  onPick,
  t,
}: {
  label: string;
  recommended: boolean;
  description: string;
  busy: boolean;
  disabled: boolean;
  onPick: () => void;
  t: (key: string) => string;
}) {
  return (
    <button
      type="button"
      onClick={onPick}
      disabled={disabled}
      className="flex flex-col items-start gap-2 rounded-[--radius-md] border border-border bg-bg-elevated p-4 text-left transition-colors hover:border-accent/60 disabled:opacity-50"
    >
      <div className="flex w-full items-center justify-between gap-2">
        <span className="font-medium text-text">{label}</span>
        <div className="flex items-center gap-2">
          {recommended && <Badge variant="success">{t("setup.database.recommended")}</Badge>}
          {busy && <Spinner size="sm" />}
        </div>
      </div>
      <p className="text-xs text-text-muted">{description}</p>
    </button>
  );
}
