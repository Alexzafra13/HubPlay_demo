import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApproveDeviceCode } from "@/api/hooks/deviceAuth";

// LinkDevice — /link route. The operator landed here from the verification
// URL displayed on a TV / CLI / headless device that's polling our
// /auth/device/poll endpoint. They paste the user_code they see on the
// device, hit Approve, and the device's next poll receives tokens.
//
// Errors mirror RFC 8628's protocol vocabulary plus our localised
// strings:
//
//   404 USER_CODE_UNKNOWN          — typo or already-cleaned-up code
//   410 USER_CODE_EXPIRED          — past 10-minute TTL; ask device to retry
//   409 USER_CODE_ALREADY_APPROVED — somebody else approved this code
//
// The page is auth-gated by App.tsx's ProtectedRoute, so claims are
// guaranteed present here.
export default function LinkDevice() {
  const { t } = useTranslation();
  const approve = useApproveDeviceCode();
  const [code, setCode] = useState("");
  const [success, setSuccess] = useState(false);

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSuccess(false);
    if (!code.trim()) return;
    approve.mutate(canonicalise(code), {
      onSuccess: () => {
        setSuccess(true);
      },
    });
  }

  return (
    <div className="mx-auto flex max-w-md flex-col gap-6 p-6 sm:p-10">
      <header>
        <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
          {t("link.title")}
        </h1>
        <p className="mt-2 text-sm text-text-muted">{t("link.subtitle")}</p>
      </header>

      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <label className="flex flex-col gap-2">
          <span className="text-sm font-medium text-text-primary">
            {t("link.codeLabel")}
          </span>
          <input
            type="text"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder="ABCD-EFGH"
            autoFocus
            spellCheck={false}
            autoComplete="off"
            inputMode="text"
            aria-describedby="link-code-hint"
            className="rounded-lg border border-border bg-bg-elevated px-4 py-3 font-mono text-2xl tracking-widest text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <span id="link-code-hint" className="text-xs text-text-muted">
            {t("link.codeHint")}
          </span>
        </label>

        <button
          type="submit"
          disabled={approve.isPending || !code.trim() || success}
          className="rounded-lg bg-accent px-4 py-3 text-base font-semibold text-bg-base disabled:cursor-not-allowed disabled:opacity-50 hover:bg-accent-hover"
        >
          {approve.isPending ? t("link.approving") : t("link.approve")}
        </button>
      </form>

      {success ? (
        <div
          role="status"
          className="rounded-lg border border-green-500/40 bg-green-500/10 p-4 text-sm text-text-primary"
        >
          {t("link.success")}
        </div>
      ) : null}

      {approve.isError ? (
        <div
          role="alert"
          className="rounded-lg border border-red-500/40 bg-red-500/10 p-4 text-sm text-text-primary"
        >
          {humaniseError(approve.error, t)}
        </div>
      ) : null}
    </div>
  );
}

// canonicalise mirrors the backend's canonicalUserCode: strip dashes
// + whitespace + uppercase. Done client-side too so the wire payload
// is always the canonical form.
function canonicalise(s: string): string {
  return s.replace(/[\s-]/g, "").toUpperCase();
}

// humaniseError maps the API error code (set as the message by our
// fetch wrapper) to a localised user-facing string. Falls back to the
// raw message so unexpected errors stay debuggable.
function humaniseError(err: Error, t: (k: string) => string): string {
  const msg = err.message || "";
  if (msg.includes("USER_CODE_UNKNOWN")) return t("link.errorUnknown");
  if (msg.includes("USER_CODE_EXPIRED")) return t("link.errorExpired");
  if (msg.includes("USER_CODE_ALREADY_APPROVED")) return t("link.errorAlreadyApproved");
  if (msg.includes("ACCOUNT_DISABLED")) return t("link.errorAccountDisabled");
  return msg;
}
