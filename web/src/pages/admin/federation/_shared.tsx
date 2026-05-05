// Small primitives shared across the federation-admin sub-pages.
// Lifted out of the original 684-LOC `FederationAdmin.tsx` so each
// section file (IdentityCard / InviteSection / AcceptSection /
// PeersTable) imports the same pieces instead of redefining them.

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/common/Button";

export function Label({ children }: { children: React.ReactNode }) {
  return <p className="text-xs uppercase tracking-wide text-text-muted">{children}</p>;
}

export function Value({ children, mono = false }: { children: React.ReactNode; mono?: boolean }) {
  return (
    <p className={`mt-1 break-all text-sm text-text-primary ${mono ? "font-mono" : ""}`}>
      {children}
    </p>
  );
}

export function FieldInput({
  label,
  placeholder,
  value,
  onChange,
}: {
  label: string;
  placeholder?: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs uppercase tracking-wide text-text-muted">{label}</span>
      <input
        type="text"
        className="rounded border border-border bg-bg-base px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}

export function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const handleClick = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Some browsers gate clipboard on insecure context (HTTP). Fall
      // back to a no-op — the user can still read the field manually.
    }
  };

  return (
    <Button variant="secondary" size="sm" onClick={handleClick}>
      {copied ? t("admin.federation.copied") : t("admin.federation.copy")}
    </Button>
  );
}

export function ErrorBanner({
  message,
  className = "",
}: {
  message: string;
  className?: string;
}) {
  return (
    <p className={`rounded border border-danger/40 bg-danger/5 p-3 text-sm text-danger ${className}`}>
      {message}
    </p>
  );
}
