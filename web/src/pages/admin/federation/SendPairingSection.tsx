import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Send } from "lucide-react";
import { useSendPairingRequest } from "@/api/hooks/federation";
import { Button, UserAvatar } from "@/components/common";
import { ErrorBanner, FieldInput } from "./_shared";

// SendPairingSection — flow "Steam-style" sin codigo de invitacion.
// El admin local pega la URL del servidor remoto y pulsa "Enviar
// peticion"; nosotros probamos al remoto + le enviamos un POST a
// su /federation/pairing-requests. El admin del remoto vera la
// peticion en su badge + inbox de notificaciones, y cuando la
// acepte ambos lados quedaran emparejados.
//
// Renderiza bare (sin <section>/h3 propio) porque vive dentro de
// un tab que ya anuncia el modo.

export function SendPairingSection() {
  const { t } = useTranslation();
  const send = useSendPairingRequest();
  const [baseURL, setBaseURL] = useState("");
  const [lastSent, setLastSent] = useState<{
    name: string;
    color?: string;
    imageURL?: string | null;
  } | null>(null);

  function handleSend() {
    const url = baseURL.trim();
    if (!url) return;
    send.mutate(url, {
      onSuccess: (req) => {
        setLastSent({
          name: req.peer_name,
          color: req.peer_avatar_color,
          imageURL: req.peer_avatar_image_url ?? null,
        });
        setBaseURL("");
      },
    });
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm leading-relaxed text-text-muted">
        {t("admin.federation.sendRequest.description", {
          defaultValue:
            "Pega la URL del servidor de tu colega y pulsa enviar. Le aparecerá una notificación en su panel para que la acepte. Sin códigos, sin pasos extra. Cuando acepte, ambos quedaréis emparejados.",
        })}
      </p>

      <div className="flex flex-col gap-3">
        <FieldInput
          label={t("admin.federation.sendRequest.urlLabel", {
            defaultValue: "URL del servidor remoto",
          })}
          placeholder="https://hubplay.tu-amigo.example.com"
          value={baseURL}
          onChange={setBaseURL}
        />
        <Button
          variant="primary"
          onClick={handleSend}
          disabled={send.isPending || !baseURL.trim()}
          isLoading={send.isPending}
        >
          <Send className="-ml-1 mr-1.5 h-4 w-4" />
          {t("admin.federation.sendRequest.send", {
            defaultValue: "Enviar petición",
          })}
        </Button>
        {send.error && <ErrorBanner message={String(send.error)} />}
      </div>

      {lastSent && (
        <div className="rounded-md border border-success/40 bg-success/5 p-4">
          <div className="flex items-start gap-3">
            <UserAvatar
              user={{
                username: lastSent.name,
                display_name: lastSent.name,
                avatar_color: lastSent.color,
                avatar_image_url: lastSent.imageURL,
              }}
              size="md"
            />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-semibold text-text-primary">
                {t("admin.federation.sendRequest.sentTo", {
                  defaultValue: "Petición enviada a {{name}}",
                  name: lastSent.name,
                })}
              </p>
              <p className="mt-1 text-xs leading-relaxed text-text-muted">
                {t("admin.federation.sendRequest.sentHint", {
                  defaultValue:
                    "El otro admin la verá en su panel. Mientras esperas su respuesta, aparecerá abajo en 'Peticiones pendientes'.",
                })}
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
