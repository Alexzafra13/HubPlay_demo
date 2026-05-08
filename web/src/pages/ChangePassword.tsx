// ChangePassword — forced post-login screen for users whose account
// has the `password_change_required` flag set. Two paths land here:
//
//   1. The admin created the user with an auto-generated temporary
//      password. First login → this screen, no escape until the user
//      rotates the credential to one only they know.
//   2. The admin pressed "Reset password" on an existing user. Same
//      flag, same screen.
//
// The current-password field is intentionally optional in the must-
// change case: the user just authenticated using the temp password
// the admin handed them, asking them to retype it adds friction
// without security gain (the JWT they're holding already proved
// possession). Toggle the "I prefer to type my old password" checkbox
// to surface the field anyway — useful when sharing the screen with
// the admin who is watching.

import { useState } from "react";
import type { FormEvent } from "react";
import { Navigate, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/store/auth";
import { useChangeMyPassword } from "@/api/hooks";
import { Button, Input } from "@/components/common";
import { BrandWordmark } from "@/components/layout/BrandWordmark";

export default function ChangePassword() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { user, isAuthenticated, refreshMe } = useAuthStore();
  const mustChange = user?.password_change_required === true;
  const [verifyCurrent, setVerifyCurrent] = useState(!mustChange);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const change = useChangeMyPassword();

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  // Voluntary visit while no rotation is pending → just go home.
  // The Settings page will eventually grow a "Change my password"
  // entry that routes here regardless.
  if (!mustChange && !user) {
    return <Navigate to="/" replace />;
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErrorMessage(null);
    if (newPassword.length < 8) {
      setErrorMessage(t("changePassword.errorTooShort"));
      return;
    }
    if (newPassword !== confirm) {
      setErrorMessage(t("changePassword.errorMismatch"));
      return;
    }
    try {
      await change.mutateAsync({
        currentPassword: verifyCurrent ? currentPassword : "",
        newPassword,
      });
      // Refresh /me so the cached must-change flag flips off, then
      // boot to home. ProtectedRoute won't bounce us back this time.
      await refreshMe?.();
      navigate("/", { replace: true });
    } catch (err) {
      const message = err instanceof Error ? err.message : t("changePassword.errorGeneric");
      setErrorMessage(message);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center px-4 bg-bg-base">
      <div className="w-full max-w-sm flex flex-col gap-6">
        <div className="flex flex-col items-center gap-4">
          <BrandWordmark height={32} />
          <div className="text-center">
            <h1 className="text-xl font-semibold text-text-primary">
              {mustChange
                ? t("changePassword.titleForced")
                : t("changePassword.title")}
            </h1>
            <p className="mt-1 text-sm text-text-muted">
              {mustChange
                ? t("changePassword.subtitleForced")
                : t("changePassword.subtitle")}
            </p>
          </div>
        </div>

        <form onSubmit={onSubmit} className="flex flex-col gap-3">
          {(verifyCurrent || !mustChange) && (
            <Input
              label={t("changePassword.currentLabel")}
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              required={!mustChange}
            />
          )}
          <Input
            label={t("changePassword.newLabel")}
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            required
            minLength={8}
          />
          <Input
            label={t("changePassword.confirmLabel")}
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            required
            minLength={8}
          />

          {mustChange && !verifyCurrent && (
            <button
              type="button"
              onClick={() => setVerifyCurrent(true)}
              className="self-start text-xs text-text-muted hover:text-text-secondary transition-colors"
            >
              {t("changePassword.alsoVerifyOld")}
            </button>
          )}

          {errorMessage && (
            <p className="text-sm text-error">{errorMessage}</p>
          )}

          <Button
            type="submit"
            isLoading={change.isPending}
            className="mt-2"
          >
            {t("changePassword.submit")}
          </Button>
        </form>
      </div>
    </div>
  );
}
