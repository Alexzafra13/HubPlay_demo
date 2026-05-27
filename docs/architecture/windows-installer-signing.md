# Windows Installer Signing — Design Document

Firma Authenticode del installer `HubPlay-Setup-*-windows-amd64.exe` vía **SignPath Foundation**. El installer ya existe (`scripts/installer.iss` + job `windows-installer` en `release.yml`); este documento cubre **cómo activar la firma**, qué necesita el operador hacer una vez, y cómo verificar el resultado.

---

## 1. Por qué firmar

Cuando el operador descarga `HubPlay-Setup-*.exe` y lo ejecuta sin firma, Windows muestra:

```
SmartScreen ha protegido tu PC
Microsoft Defender SmartScreen ha impedido el inicio de una aplicación no reconocida.
[Más información] [No ejecutar]
```

El binario funciona si el usuario hace clic en **"Más información" → "Ejecutar de todas formas"**, pero:

- La fricción reduce la adopción dramáticamente (estudios internos de Plex/Jellyfin muestran ~30% de abandono en este paso).
- Algunos antivirus (Bitdefender, ESET) bloquean binarios sin firma directamente sin opción de bypass.
- Las políticas corporativas a menudo prohiben ejecutar binarios sin firma.

Con firma Authenticode válida:

- El nombre del publisher (`SignPath Foundation`) aparece en el UAC prompt en vez de `Publisher: Unknown`.
- SmartScreen sigue mostrando aviso al principio (reputation-based — necesita ~5000 descargas para desaparecer), pero el mensaje es menos alarmante (`Más información → Ejecutar de todas formas` sigue ahí, no es bloqueo duro).
- Antivirus respetan binarios firmados por una CA reconocida.

---

## 2. SignPath Foundation — qué es

**SignPath Foundation** ([signpath.org](https://signpath.org/)) es una iniciativa que firma binarios de proyectos open-source **gratis**. Hospedan el certificado en un HSM en la nube y los proyectos OSS lo usan via API desde sus pipelines CI/CD.

Lo usan, entre otros: Notepad++, OBS Studio, KeePass, Sumatra PDF, ShareX, Greenshot.

### Diferencia frente a otras opciones

| Opción | Coste | UX | Setup |
|---|---|---|---|
| **SignPath Foundation** | **Gratis** | SmartScreen warning al principio, desaparece con descargas | 10 min formulario + 1-2 sem espera |
| SignPath comercial | ~$10/mes | Igual | Igual + facturación |
| Cert OV propio (Sectigo/DigiCert) | $200-450/año | Igual al principio | Compra + verificación legal + HSM USB |
| Cert EV (DigiCert/GlobalSign) | $400-800/año | Sin SmartScreen warning desde día 1 | Compra + DUNS + HSM físico |
| Self-signed | Gratis | SmartScreen bloquea | — |

Para HubPlay (proyecto OSS en GitHub público), **SignPath Foundation** es la opción correcta.

---

## 3. Aplicación (una vez)

### 3.1 Requisitos del proyecto

- Repo público en GitHub (✅ HubPlay_demo ya lo es).
- Licencia OSS reconocida (✅ proyecto MIT/Apache/GPL).
- Mantenedor identificable (✅ Alexzafra13).
- No haber sido rechazado previamente por la fundación.
- No distribuir malware, mineros, herramientas de evasión, etc. (✅).

### 3.2 Aplicar

1. Ir a [signpath.org/apply](https://signpath.org/apply).
2. Rellenar el formulario:
   - Project name: `HubPlay`
   - GitHub URL: `https://github.com/Alexzafra13/HubPlay_demo`
   - Descripción breve (1-2 frases): "Self-hosted media server (Go + React) Plex/Jellyfin-style".
   - Maintainer email + GitHub handle.
   - Plataformas target: Windows.
3. Aceptar términos.
4. Esperar respuesta por email (típicamente 1-2 semanas; pueden pedir aclaraciones).

### 3.3 Si te aprueban

Recibes email con:

- Una invitación al SignPath dashboard ([app.signpath.io](https://app.signpath.io)).
- El `organization-id` (UUID) — público, va a una repo variable.
- Un proyecto preconfigurado con un `project-slug` (típicamente `hubplay`).

---

## 4. Configuración del proyecto en SignPath dashboard

Una vez con acceso al dashboard:

### 4.1 Signing Policy

Crear (o usar la pre-creada) una **signing policy**. Recomendado:

- **Slug**: `release-signing`
- **Approval mode**: Manual (al principio) — un humano (tú) confirma cada firma desde el SignPath UI. Tras 2-3 releases sin sorpresas, puedes pasarlo a **Auto** para eliminar la espera manual.
- **Allowed file types**: `.exe`, `.dll`, `.msi` (cubre Inno Setup output).
- **Code signing certificate**: el que SignPath Foundation te asigna.

### 4.2 Artifact Configuration

El installer es un `.exe` Inno Setup que internamente contiene comprimidos otros `.exe` (hubplay.exe, ffmpeg.exe, ffprobe.exe, nssm.exe). SignPath puede firmar:

- **Opción A — Solo el .exe externo**: el más simple. Es lo que el usuario ejecuta. Los .exe internos quedan sin firmar pero el operador nunca los ve directamente (los ejecuta el installer/NSSM).
- **Opción B — Nested signing**: firmar también los .exe internos antes de empaquetar. Requiere `artifact-configuration-slug` específico que descomprima, firme, recomprima. Más completo pero más compleja la policy.

**Recomendación inicial**: Opción A. Migrar a B sólo si AVs internos lo piden.

Configurar Artifact Configuration en el dashboard con slug `installer-exe` (o el default).

### 4.3 Trusted Build System

Vincular el repo de GitHub:

- En SignPath dashboard → Project → **Trusted Build Systems** → Add → GitHub Actions.
- Pega la URL del repo (`https://github.com/Alexzafra13/HubPlay_demo`).
- SignPath valida que el workflow viva en el branch principal y use la action oficial.

### 4.4 API Token

Generar el token que GitHub Actions usará para autenticarse:

- Dashboard → User settings → **API Tokens** → Create token.
- Scope: solo el proyecto HubPlay.
- Validez: 1 año (renovable).
- **Guardar inmediatamente** — sólo se ve una vez.

---

## 5. Configuración de GitHub Actions

Necesitas 1 secret + 4 variables.

### 5.1 Repo secret

`Settings → Secrets and variables → Actions → Secrets → New repository secret`:

| Nombre | Valor |
|---|---|
| `SIGNPATH_API_TOKEN` | el token generado en 4.4 |

### 5.2 Repo variables

`Settings → Secrets and variables → Actions → Variables → New repository variable`:

| Nombre | Valor | Notas |
|---|---|---|
| `HUBPLAY_SIGNING_ENABLED` | `true` | Master switch. Mantener en `false` (o no existir) hasta que todo lo demás esté listo. |
| `SIGNPATH_ORGANIZATION_ID` | UUID del email de aprobación | Público, no es secret. |
| `SIGNPATH_PROJECT_SLUG` | `hubplay` (o el slug que te asignen) | Visible en la URL del proyecto en el dashboard. |
| `SIGNPATH_SIGNING_POLICY_SLUG` | `release-signing` | El slug que creaste en 4.1. |

### 5.3 Por qué vars y no secrets

Los UUIDs y slugs **no son sensibles** — alguien que los vea no puede firmar nada sin el `SIGNPATH_API_TOKEN`. Ponerlos como variables permite verlos en logs (debug), cambiarlos sin re-encriptar, y dejarlos referenciables desde otros workflows si crece el setup.

---

## 6. Activar la firma

Una vez configurados secret + variables, **no hay cambios de código necesarios** — el workflow ya tiene el step condicionado. Próximo build (tag `v*` o push a `main`):

1. Inno Setup compila `HubPlay-Setup-vX.Y.Z-windows-amd64.exe`.
2. El workflow detecta `vars.HUBPLAY_SIGNING_ENABLED == 'true'`.
3. Sube el `.exe` como artifact intermedio.
4. Invoca `signpath/github-action-submit-signing-request@v2` con el artifact-id.
5. SignPath descarga, valida, firma (manual approval si la policy lo pide).
6. El action descarga el `.exe` firmado y lo deja en `dist/` (sobreescribiendo el original).
7. El step "Hash the installer" calcula el sha256 de la versión firmada.
8. Upload final del artifact + adjunto al release.

Tiempo añadido al workflow: ~30-60 s si la policy es Auto. Si es Manual, esperará a que tú apruebes desde el SignPath dashboard (puede ser horas).

---

## 7. Verificar localmente

Después del primer release firmado, descarga el `.exe` y verifica:

### 7.1 Visualmente (Explorer)

Botón derecho sobre el `.exe` → **Propiedades** → pestaña **Firmas digitales**. Debe mostrar:

```
Nombre del firmante: SignPath Foundation
Algoritmo de resumen: sha256
Timestamp: <fecha de la firma>
```

### 7.2 PowerShell

```powershell
Get-AuthenticodeSignature .\HubPlay-Setup-v0.1.0-windows-amd64.exe
```

Expected output:

```
SignerCertificate      Status                                 Path
-----------------      ------                                 ----
[abcdef...]            Valid                                  HubPlay-Setup-v0.1.0-windows-amd64.exe
```

`Status: Valid` significa firma correcta + cadena de confianza completa hasta una root CA del sistema.

### 7.3 signtool (verbose, debug)

Si tienes Windows SDK:

```powershell
signtool verify /pa /v HubPlay-Setup-v0.1.0-windows-amd64.exe
```

Muestra la cadena de certificados completa, el timestamping, y cualquier warning de policy.

---

## 8. Troubleshooting

### "SignPath rejected the application"

Razones comunes:

- **Sin LICENSE clara** en el repo → añadir LICENSE en raíz.
- **Maintainer no identificable** → poner email/contacto en README.
- **Proyecto considerado dual-use** (herramientas de red, packet sniffers, etc.) → no aplica a HubPlay.
- **Volumen de descargas muy bajo** (proyecto recién creado) → re-aplicar tras unos meses con tracción.

### "API token returned 401"

- Token expirado → regenerar en SignPath dashboard.
- Token con scope incorrecto → asegúrate que es para "Submit Signing Request", no solo lectura.

### "Artifact not found" en el step de SignPath

- El step previo "Upload unsigned installer for SignPath" no corrió (variable mal escrita).
- `artifact-id` no se está propagando — comprueba que el step tiene `id: upload-unsigned` y el siguiente referencia `${{ steps.upload-unsigned.outputs.artifact-id }}`.

### Firma queda colgada (`wait-for-completion` timeout)

- Policy en modo Manual y nadie aprobó → ir al dashboard y aprobar, o cambiar policy a Auto.
- Aumentar `wait-for-completion-timeout-in-seconds` (default 600 = 10 min) si tu policy tarda más.

### El .exe firmado pesa más / sha256 cambia entre runs

Esperado. Authenticode añade ~5-20 KB al binario (cadena de certificados + timestamp). El sha256 se calcula DESPUÉS de la firma — por eso el sha256 del release Y el del .exe descargado coinciden.

---

## 9. Desactivar firma (rollback)

Si en algún momento quieres volver a builds sin firmar (mantenimiento de SignPath, problema temporal):

```
Settings → Secrets and variables → Actions → Variables
Edit HUBPLAY_SIGNING_ENABLED → set to "false"
```

El siguiente build saltará todos los steps de SignPath y publicará el `.exe` sin firmar. Los release notes vuelven al texto "Aviso de SmartScreen" automáticamente.

---

## 10. Coste futuro / alternativas

**SignPath Foundation seguirá siendo gratis** mientras HubPlay siga siendo OSS público. Si en algún momento el proyecto se vuelve cerrado o comercial:

- **SignPath comercial**: desde $10/mes — misma API, mismo flow, sólo cambia el contrato.
- **Cert EV propio**: $400-800/año — sin SmartScreen warning desde día 1, pero requiere HSM físico y gestión propia del cert.
- **Azure Trusted Signing**: ~$10/mes desde Octubre 2024 — Microsoft-managed, integra con GitHub Actions, requiere DUNS o equivalente. Atractivo para proyectos comerciales.

Ninguna decisión pendiente — la migración es transparente al workflow (cambian solo los secrets/vars).

---

## Referencias

- SignPath Foundation: [signpath.org](https://signpath.org/)
- Action oficial: [github.com/SignPath/github-action-submit-signing-request](https://github.com/SignPath/github-action-submit-signing-request)
- Docs SignPath: [docs.signpath.io](https://docs.signpath.io)
- Inno Setup script: [`scripts/installer.iss`](../../scripts/installer.iss)
- Workflow: [`.github/workflows/release.yml`](../../.github/workflows/release.yml) → job `windows-installer`
