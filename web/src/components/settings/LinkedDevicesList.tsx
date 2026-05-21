import { useTranslation } from "react-i18next";
import { Tv } from "lucide-react";
import type { MySession } from "@/api/types";
import { useRevokeMySession } from "@/api/hooks";
import { DeviceRow } from "./DeviceRow";

// LinkedDevicesList — narrowed roster shown on the /link page below
// the user_code form. Filters MySession to the rows the device-code
// flow minted (auth_method === "device_link") so the operator only
// sees TVs / consoles / CLI tools they've paired, not every web login.
// Settings → Dispositivos remains the full roster.
//
// Hidden entirely when there are no device-linked sessions — the
// /link page is the entry point for FIRST-time pairings too and an
// empty stub would just add visual noise on the happy path.
export function LinkedDevicesList({ sessions }: { sessions: MySession[] }) {
  const { t } = useTranslation();
  const revoke = useRevokeMySession();

  const linked = sessions.filter((s) => s.auth_method === "device_link");
  if (linked.length === 0) {
    return null;
  }

  const handleRevoke = (s: MySession) => {
    revoke.mutate({ sessionId: s.id });
  };

  return (
    <section
      aria-labelledby="linked-devices-heading"
      className="mt-2 flex flex-col gap-3"
    >
      <header className="flex items-center gap-2">
        <Tv className="size-4 text-text-secondary" aria-hidden />
        <h2
          id="linked-devices-heading"
          className="text-sm font-semibold text-text-primary"
        >
          {t("link.linked.title", { defaultValue: "Dispositivos vinculados" })}
        </h2>
        <span className="text-xs text-text-muted">({linked.length})</span>
      </header>
      <p className="text-xs text-text-muted">
        {t("link.linked.subtitle", {
          defaultValue:
            "Aparatos que ya autorizaste desde aquí. Cierra la sesión de uno si lo prestaste o si ya no lo usas.",
        })}
      </p>
      <ul className="flex flex-col divide-y divide-border-subtle rounded-[--radius-lg] border border-border bg-bg-card">
        {linked.map((s) => (
          <DeviceRow
            key={s.id}
            session={s}
            onRevoke={() => handleRevoke(s)}
            // The /link list is already filtered, so an "auth method"
            // badge would be redundant here — hide it.
            showAuthMethodBadge={false}
          />
        ))}
      </ul>
    </section>
  );
}
