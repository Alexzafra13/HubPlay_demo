import { useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useSetupCreateAdmin } from "@/api/hooks";
import { useAuthStore } from "@/store/auth";
import { Button, Input } from "@/components/common";
import { ApiError } from "@/api/types";
import { api } from "@/api/client";

// ─── Types ───────────────────────────────────────────────────────────────────

interface AdminData {
  username: string;
  password: string;
  displayName?: string;
}

interface AccountStepProps {
  onNext: (data: AdminData) => void;
  initialData?: AdminData;
}

// ─── Component ───────────────────────────────────────────────────────────────

export default function AccountStep({ onNext, initialData }: AccountStepProps) {
  const { t } = useTranslation();
  const createAdmin = useSetupCreateAdmin();
  const setAuth = useAuthStore((s) => s.setAuth);

  const [username, setUsername] = useState(initialData?.username ?? "");
  const [password, setPassword] = useState(initialData?.password ?? "");
  const [confirmPassword, setConfirmPassword] = useState(
    initialData?.password ?? "",
  );
  const [displayName, setDisplayName] = useState(
    initialData?.displayName ?? "",
  );

  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<string | null>(null);

  function validate(): boolean {
    const newErrors: Record<string, string> = {};

    if (username.trim().length < 3) {
      newErrors.username = t("setup.account.usernameMinLength");
    }

    if (password.length < 8) {
      newErrors.password = t("setup.account.passwordMinLength");
    }

    if (password !== confirmPassword) {
      newErrors.confirmPassword = t("setup.account.passwordMismatch");
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setServerError(null);

    if (!validate()) return;

    createAdmin.mutate(
      {
        username: username.trim(),
        password,
        display_name: displayName.trim() || undefined,
      },
      {
        onSuccess(data) {
          setAuth(data.user, data.access_token, data.refresh_token);
          onNext({
            username: username.trim(),
            password,
            displayName: displayName.trim() || undefined,
          });
        },
        async onError(err) {
          // Admin already exists — try logging in instead
          if (err instanceof ApiError && err.code === "SETUP_COMPLETED") {
            try {
              const loginData = await api.login(username.trim(), password);
              setAuth(loginData.user, loginData.access_token, loginData.refresh_token);
              onNext({
                username: username.trim(),
                password,
                displayName: displayName.trim() || undefined,
              });
              return;
            } catch {
              setServerError(t("setup.account.adminExists"));
              return;
            }
          }
          setServerError(
            err.message || "Failed to create admin account. Please try again.",
          );
        },
      },
    );
  }

  return (
    <div>
      <div className="mb-6">
        <h2 className="text-xl font-semibold text-text-primary">
          {t("setup.account.title")}
        </h2>
        <p className="mt-1 text-sm text-text-secondary">
          {t("setup.account.description")}
        </p>
      </div>

      {/* Warning */}
      <div className="mb-6 flex items-start gap-3 rounded-[--radius-md] bg-warning/10 px-4 py-3">
        <svg
          className="mt-0.5 h-5 w-5 shrink-0 text-warning"
          viewBox="0 0 20 20"
          fill="currentColor"
        >
          <path
            fillRule="evenodd"
            d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.168 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495zM10 6a.75.75 0 01.75.75v3.5a.75.75 0 01-1.5 0v-3.5A.75.75 0 0110 6zm0 9a1 1 0 100-2 1 1 0 000 2z"
            clipRule="evenodd"
          />
        </svg>
        <p className="text-sm text-warning">
          {t("setup.account.adminNote")}
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <Input
          label={t("setup.account.username")}
          type="text"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder={t("setup.account.usernamePlaceholder")}
          autoComplete="username"
          error={errors.username}
          required
        />

        <Input
          label={t("setup.account.displayName")}
          type="text"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("setup.account.displayNamePlaceholder")}
          hint={t("setup.account.displayNameHint")}
        />

        <Input
          label={t("setup.account.password")}
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder={t("setup.account.passwordPlaceholder")}
          autoComplete="new-password"
          error={errors.password}
          required
        />

        <Input
          label={t("setup.account.confirmPassword")}
          type="password"
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          placeholder={t("setup.account.confirmPlaceholder")}
          autoComplete="new-password"
          error={errors.confirmPassword}
          required
        />

        {serverError && (
          <p className="rounded-[--radius-sm] bg-error/10 px-3 py-2 text-sm text-error">
            {serverError}
          </p>
        )}

        <div className="mt-2 flex justify-end">
          <Button
            type="submit"
            size="lg"
            isLoading={createAdmin.isPending}
          >
            {t("setup.account.submit")}
          </Button>
        </div>
      </form>
    </div>
  );
}
