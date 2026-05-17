import { useState } from "react";
import type { FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useChangeMyPassword } from "@/api/hooks";
import { Button, Input } from "@/components/common";

// Panel "Cambiar contraseña" — formulario auto-contenido. El backend
// ya tiene POST /me/password con validación. Aquí sólo controlamos
// que las dos casillas de la nueva coincidan antes de mandar; el
// resto (longitud mínima, política) lo enforce el server y lo
// surfaceamos con setError.
export function PasswordPanel() {
  const { t } = useTranslation();
  const changePassword = useChangeMyPassword();

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!current || !next) {
      setError(
        t("settings.passwordPanel.fieldsRequired", {
          defaultValue: "Rellena la contraseña actual y la nueva.",
        }),
      );
      return;
    }
    if (next !== confirm) {
      setError(
        t("settings.passwordPanel.mismatch", {
          defaultValue: "La nueva contraseña y la confirmación no coinciden.",
        }),
      );
      return;
    }
    setError(null);
    setSaved(false);
    changePassword.mutate(
      { currentPassword: current, newPassword: next },
      {
        onSuccess: () => {
          setCurrent("");
          setNext("");
          setConfirm("");
          setSaved(true);
        },
        onError: (err) => setError(err.message),
      },
    );
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-[--radius-lg] border border-border bg-bg-card p-6 flex flex-col gap-4"
    >
      <div className="flex flex-col gap-1">
        <h3 className="text-base font-semibold text-text-primary">
          {t("settings.passwordPanel.title", {
            defaultValue: "Cambiar contraseña",
          })}
        </h3>
        <p className="text-xs text-text-muted">
          {t("settings.passwordPanel.hint", {
            defaultValue:
              "Te pedirá la actual para confirmar. La nueva se guarda inmediatamente.",
          })}
        </p>
      </div>

      <Input
        type="password"
        autoComplete="current-password"
        label={t("settings.passwordPanel.current", {
          defaultValue: "Contraseña actual",
        })}
        value={current}
        onChange={(e) => setCurrent(e.target.value)}
      />
      <Input
        type="password"
        autoComplete="new-password"
        label={t("settings.passwordPanel.new", {
          defaultValue: "Nueva contraseña",
        })}
        value={next}
        onChange={(e) => setNext(e.target.value)}
      />
      <Input
        type="password"
        autoComplete="new-password"
        label={t("settings.passwordPanel.confirm", {
          defaultValue: "Confirmar nueva contraseña",
        })}
        value={confirm}
        onChange={(e) => setConfirm(e.target.value)}
      />

      {error && <p className="text-xs text-error">{error}</p>}
      {saved && (
        <p className="text-xs text-success">
          {t("settings.passwordPanel.saved", {
            defaultValue: "Contraseña actualizada.",
          })}
        </p>
      )}

      <div className="flex justify-end">
        <Button
          type="submit"
          isLoading={changePassword.isPending}
          disabled={!current || !next || !confirm}
        >
          {t("settings.passwordPanel.cta", {
            defaultValue: "Cambiar contraseña",
          })}
        </Button>
      </div>
    </form>
  );
}
