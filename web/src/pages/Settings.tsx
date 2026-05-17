import { useTranslation } from "react-i18next";
import { HomeLayoutSettings } from "@/components/home";
import { AccountPanel } from "@/components/settings/AccountPanel";
import { DevicesPanel } from "@/components/settings/DevicesPanel";
import { PasswordPanel } from "@/components/settings/PasswordPanel";
import { PlaybackSettings } from "@/components/settings/PlaybackSettings";

// Settings — preferencias del propio usuario. Aquí NO viven cosas de
// servidor (proveedores de metadatos, bibliotecas, escaneo): eso
// pertenece al panel admin para que un usuario normal no vea
// chrome que no le aplica y para que el admin no tenga el mismo
// botón en dos sitios distintos.
//
// Orden: lo más "tu identidad" arriba (Mi cuenta, contraseña) y
// detrás las preferencias de uso (inicio, reproducción, sesiones).
export default function Settings() {
  const { t } = useTranslation();

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-8 px-6 py-8 sm:px-10">
      <h1 className="text-2xl font-bold text-text-primary sm:text-3xl">
        {t("settings.title")}
      </h1>

      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          {t("settings.account")}
        </h2>
        <AccountPanel />
        <PasswordPanel />
      </section>

      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          {t("settings.homeLayout.title", { defaultValue: "Personalizar inicio" })}
        </h2>
        <HomeLayoutSettings />
      </section>

      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          {t("settings.playback.title")}
        </h2>
        <PlaybackSettings />
      </section>

      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-text-primary">
          {t("settings.devices.title", { defaultValue: "Tus dispositivos" })}
        </h2>
        <p className="text-sm text-text-muted">
          {t("settings.devices.subtitle", {
            defaultValue:
              "Cada inicio de sesión queda guardado aquí. Cierra una sesión si no reconoces el dispositivo o pierdes el móvil.",
          })}
        </p>
        <DevicesPanel />
      </section>
    </div>
  );
}
