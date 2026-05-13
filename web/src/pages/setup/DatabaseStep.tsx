// DatabaseStep — wizard step 0.
//
// Before the operator creates the first admin, before any library
// has been added, the wizard now asks "where do you want the data
// to live?". The default answer is "the bundled SQLite file at the
// path your YAML already points at", but operators planning a
// production deployment with Postgres can pick it here without
// editing hubplay.yaml on disk.
//
// When the operator changes the backend, the wizard:
//   1. Tests the connection (Open + Ping).
//   2. On success, persists the new driver/DSN to hubplay.yaml.
//   3. Restarts the server so the next boot opens the new backend.
//   4. The /setup/status poll then resumes the wizard from "account".
//
// Skip is a first-class affordance: the SQLite default is fine for
// 90 % of self-hosted installs, and dropping into a DSN form on the
// very first screen of a fresh install is a UX foot-gun if you
// don't read the help text. The "Continue with SQLite" button skips
// straight to the next step without any test/save round-trip.

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Database } from "lucide-react";

import { api } from "@/api/client";
import type {
  AdminDatabaseTestResponse,
  DatabaseDriver,
} from "@/api/types";
import { Button, Input, Spinner } from "@/components/common";

interface DatabaseStepProps {
  onNext: () => void;
}

export default function DatabaseStep({ onNext }: DatabaseStepProps) {
  const { t } = useTranslation();

  const [driver, setDriver] = useState<DatabaseDriver>("sqlite");
  const [dsn, setDsn] = useState("");
  const [path, setPath] = useState("");
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<AdminDatabaseTestResponse | null>(null);
  const [savedBanner, setSavedBanner] = useState<string | null>(null);

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
      // The server will go down within ~100 ms; subsequent requests
      // will fail until the container comes back. We don't navigate
      // because /setup/status on the next page load will detect the
      // wizard is still active (no users + no libraries) and resume
      // here. The user sees the "restarting…" banner during the
      // ~2-3 second window.
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
          <Database className="h-6 w-6" />
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
            testResult.ok
              ? "bg-success/10 text-success"
              : "bg-danger/10 text-danger",
          ].join(" ")}
        >
          {testResult.ok ? (
            <span>
              ✓ {t("setup.database.testOK")}
              {testResult.server_version
                ? ` — ${testResult.server_version}`
                : ""}{" "}
              <span className="text-text-muted">({testResult.duration_ms} ms)</span>
            </span>
          ) : (
            <span>✗ {testResult.error}</span>
          )}
        </div>
      )}

      {savedBanner && (
        <div
          role="status"
          className="rounded-[--radius-sm] bg-info/10 px-3 py-2 text-sm text-info"
        >
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
    </div>
  );
}
