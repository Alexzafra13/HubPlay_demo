import { useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useLogin } from "@/api/hooks";
import { useAuthStore } from "@/store/auth";
import { Button } from "@/components/common";
import { Input } from "@/components/common";

// Login — visual rework 2026-04-30. The previous version was a card
// floating on flat bg-base; the first impression of the product was
// "this is a generic admin tool". Now: an aurora backdrop (same
// vocabulary as the detail-page ambient effect — three radial gradients
// with the project's accent palette) + glass-effect card on top.
//
// Why CSS gradients and not a backdrop image: the user is unauthenticated,
// so they can't fetch any item / poster from the API yet. A static asset
// in the bundle would work but ages badly (one image gets boring).
// Aurora is on-brand (matches detail pages), pure CSS, and stays fresh
// because the colours are close to the accent token — change the token,
// the login backdrop changes too.
export default function Login() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const login = useLogin();
  const setAuth = useAuthStore((s) => s.setAuth);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    login.mutate(
      { username, password },
      {
        onSuccess(data) {
          setAuth(data.user);
          navigate("/");
        },
        onError(err) {
          setError(err.message || t("login.loginFailed"));
        },
      },
    );
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-bg-base px-4">
      {/* Aurora backdrop. Three radial gradients stacked: a vibrant
          teal blob upper-left, a deeper teal lower-right, and a small
          warm halo center-bottom to break the monochrome. The whole
          layer sits behind the card with -z-10 and is wider than the
          viewport so it never reveals an edge on ultrawide. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10"
        style={{
          background: [
            // upper-left vibrant (accent teal)
            "radial-gradient(60% 70% at 18% 22%, rgba(45, 212, 191, 0.35) 0%, transparent 65%)",
            // lower-right muted (accent-soft, deeper)
            "radial-gradient(55% 65% at 82% 78%, rgba(13, 148, 136, 0.28) 0%, transparent 60%)",
            // small warm halo to add temperature contrast
            "radial-gradient(35% 40% at 50% 95%, rgba(244, 114, 182, 0.10) 0%, transparent 70%)",
          ].join(", "),
        }}
      />

      {/* Subtle vignette so the card edges read against the glow */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 bg-gradient-to-b from-transparent via-bg-base/40 to-bg-base/80"
      />

      {/* Card. backdrop-blur over a semi-transparent surface so the
          aurora hue tints through. The border stays high-contrast on
          the dark bg without being garish. */}
      <div className="relative w-full max-w-sm rounded-2xl border border-white/10 bg-bg-card/70 p-8 shadow-2xl backdrop-blur-xl">
        <div className="mb-7 flex flex-col items-center gap-3">
          <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-accent shadow-[0_0_30px_rgba(13,148,136,0.55)]">
            <svg
              className="h-8 w-8 text-white"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
          <div className="flex flex-col items-center gap-1">
            <h1 className="text-3xl font-bold tracking-tight text-text-primary">
              HubPlay
            </h1>
            <p className="text-xs text-text-muted">{t("login.tagline")}</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Input
            label={t("login.username")}
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder={t("login.usernamePlaceholder")}
            autoComplete="username"
            required
          />

          <Input
            label={t("login.password")}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={t("login.passwordPlaceholder")}
            autoComplete="current-password"
            required
          />

          {error && (
            <p
              role="alert"
              className="rounded-md border border-error/30 bg-error/10 px-3 py-2 text-sm text-error"
            >
              {error}
            </p>
          )}

          <Button
            type="submit"
            fullWidth
            size="lg"
            isLoading={login.isPending}
            className="mt-2"
          >
            {t("login.signIn")}
          </Button>
        </form>
      </div>
    </div>
  );
}
