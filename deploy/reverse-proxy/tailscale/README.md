# Tailscale — mesh privada para amigos-only

Si tu HubPlay es para ti y un puñado de amigos (que es exactamente el caso de uso de federación entre dos servidores), Tailscale es probablemente la mejor opción. Cero exposición a internet público, certificados HTTPS automáticos vía MagicDNS, traversal NAT que "simplemente funciona".

> Tailscale es **gratis** hasta 100 dispositivos en tu tailnet. Más que suficiente para una federación de amigos.

## Por qué Tailscale para federación

- **NAT traversal nativo**: ambos peers detrás de CGNAT, sin port-forwarding, conectan igual.
- **HTTPS automático**: cada peer tiene URL `https://nombre.tailnet-XYZ.ts.net` con cert válido emitido por Tailscale (vía Let's Encrypt detrás de bambalinas, sin que toques nada).
- **Identity built-in**: tu tailnet ya autentica a cada nodo, lo que añade una segunda capa de identidad sobre el Ed25519 de federación.
- **Privacy**: nada de tu HubPlay sale a internet público. Tu IP queda invisible. Tus peers ven sólo el endpoint Tailscale.
- **Zero config DNS**: MagicDNS resuelve los nombres dentro de tu tailnet sin que toques DNS público.

## Setup en cada peer

```bash
# 1. Instalar Tailscale.
curl -fsSL https://tailscale.com/install.sh | sh

# 2. Login y conectar al tailnet.
sudo tailscale up

# 3. Habilita HTTPS en MagicDNS.
sudo tailscale cert hubplay.tu-tailnet.ts.net
# Esto descarga el cert + key a /var/lib/tailscale/certs/

# 4. Permitir tráfico HTTPS de los peers de tu tailnet.
sudo tailscale serve https / http://127.0.0.1:8096
# Esto monta un reverse-proxy interno en Tailscale apuntando a HubPlay.
```

`tailscale serve` actúa como un mini-proxy interno: termina TLS con el cert que Tailscale gestiona, y proxea a HubPlay en localhost. Es la opción **más turnkey** de todo este directorio — cero configuración por tu parte fuera de un comando.

## Acceso

Desde cualquier dispositivo del mismo tailnet (tu móvil, otro server, el portátil):
```
https://hubplay.tu-tailnet.ts.net
```

Browser-friendly, cert válido, sin "tap to accept self-signed".

## Federación entre dos tailnets

Para federar tu HubPlay con el de tu amigo:

### Opción A — Tailnets separados, Tailscale Funnel (público controlado)

Tailscale Funnel (gratis con plan personal) te permite exponer un servicio Tailscale a internet público bajo el dominio `*.ts.net`. Tu peer no necesita estar en tu tailnet:

```bash
sudo tailscale funnel 443 on
# Tu HubPlay ahora es accesible públicamente en
# https://hubplay.tu-tailnet.ts.net
# pero el tráfico sigue terminando en tu host vía Tailscale.
```

Tu amigo configura su HubPlay con `Peer URL: https://hubplay.tu-tailnet.ts.net` desde fuera de su tailnet.

### Opción B — Tailnets compartidos (privacidad máxima)

Invitas a tu amigo a tu tailnet (o aceptas su invite al suyo). Ambos peers están en el mismo tailnet, ven el otro como un nodo más, sin tráfico saliendo nunca a internet público:

```bash
# Tu lado (admin del tailnet).
tailscale invite create
# Compartes el URL con tu amigo.

# Su lado.
sudo tailscale up --auth-key TU-INVITE-URL
```

Después de aceptar, ambos HubPlay se ven en `https://hubplay-alex.tu-tailnet.ts.net` y `https://hubplay-pedro.tu-tailnet.ts.net`. Configuras peering con esas URLs. Cero internet público, cero CF, cero CGNAT, cero abrir puertos.

**Esta es la configuración más privada y simple para federación entre amigos.**

### Opción C — Mesh entre tailnets (máxima escala futura)

Si tu federación crece más allá de 2-3 amigos, Tailscale tiene "tailnet sharing" para compartir nodos individuales entre tailnets sin invitar al admin completo. Útil para "te comparto sólo mi HubPlay, no veas el resto de mi homelab".

## Verificación

```bash
# Desde el mismo host.
curl -k https://hubplay.tu-tailnet.ts.net/api/v1/health   # 200 OK

# Desde otro nodo del tailnet (móvil con Tailscale, otro server).
curl https://hubplay.tu-tailnet.ts.net/api/v1/health       # 200 OK, cert válido
```

## Federación con Tailscale — particularidades

1. **TLS pinning per-peer está desactivado por defecto**: Tailscale gestiona el cert, así que CA pública del tailnet (Let's Encrypt) lo firma. Verificación TLS estándar funciona. No necesitas pinning manual.

2. **Advertised URL**: en la config `federation.public_url` de HubPlay pones el endpoint Tailscale (`https://hubplay.tu-tailnet.ts.net`). Tu peer guarda ese URL como `peer.url`. Cuando tu peer hace request, sale por su Tailscale, encruza por el mesh, llega a tu host. Cero internet público.

3. **Sin rate limiting WAN necesario**: el tailnet ya autentica cada nodo. WAN-style rate limiting es redundante. Sólo el per-peer token bucket de la app aplica.

4. **Backups de configuración**: si reinstalas el host, perderás el `nodekey` de Tailscale; el nodo aparece como nuevo. Esto re-emite el cert pero CAMBIA el fingerprint TLS si tienes pinning. Solución: usa **Tailscale ACLs** + `--auth-key` reusable para restaurar identidad consistente.

## Troubleshooting

**`tailscale serve` no devuelve nada**: el feature está detrás de un flag en algunas versiones. `tailscale serve status` te dice qué hay configurado. Update a Tailscale 1.42+ si es muy viejo.

**Cert no se obtiene**: tu tailnet no tiene MagicDNS habilitado. Activa en https://login.tailscale.com → DNS → MagicDNS.

**Otro peer fuera del tailnet no puede acceder**: ese es el punto de Tailscale. Si quieres exponer públicamente, añade Tailscale Funnel (Opción A arriba) o usa otro proxy de este directorio.

## Cuándo NO usar Tailscale

- Si tu HubPlay tiene que servir contenido a usuarios random de internet (no amigos definidos), Tailscale + Funnel se complica vs Caddy + nginx normales.
- Si no confías que Tailscale (la empresa) gestione tu mesh — es propietario, aunque el cliente es open-source. Alternativa libre: **Headscale** (Tailscale server self-hosted) + clientes Tailscale apuntando ahí.
