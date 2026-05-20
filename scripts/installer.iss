; HubPlay — Inno Setup script
;
; Genera HubPlay-Setup-<version>-windows-amd64.exe a partir de los
; archivos del build de Windows. Lo invoca el workflow Release tras
; el paso de bundle de ffmpeg, usando ISCC.exe (CLI de Inno Setup
; disponible en runner windows-latest via Minionguyjpro/Inno-Setup-Action).
;
; Estructura esperada al invocar (relativo a scripts/installer.iss):
;   ../stage/hubplay-<version>-windows-amd64/
;     hubplay.exe
;     ffmpeg.exe
;     ffprobe.exe
;     hubplay.example.yaml
;     LICENSE
;     LICENSE-ffmpeg.txt
;   ../web/public/pwa-512x512.png   (icono para Add/Remove Programs)
;
; Variables que el workflow pasa por línea de comandos:
;   /DAppVersion=v0.1.0
;   /DSourceDir=<absoluto a stage/hubplay-...-windows-amd64>
;   /DOutputDir=<destino del .exe generado>
;
; El installer registra hubplay como servicio de Windows usando
; nssm.exe que también shipea (NSSM es BSD-licensed, libre redistribución).
; Servicio se llama "HubPlay" y arranca automáticamente al boot —
; el operador no ve consola; abre el icono PWA del escritorio y entra
; al panel.

; Valores del build via env vars (las /D dan problemas con el quoting
; de la action). Si no están, fallback dev.
#define AppVersion GetEnv("HUBPLAY_APP_VERSION")
#if AppVersion == ""
  #define AppVersion "dev"
#endif
#define SourceDir GetEnv("HUBPLAY_SOURCE_DIR")
#if SourceDir == ""
  #define SourceDir "..\stage\hubplay-dev-windows-amd64"
#endif
#define OutputDir GetEnv("HUBPLAY_OUTPUT_DIR")
#if OutputDir == ""
  #define OutputDir "..\dist"
#endif

[Setup]
AppId={{B5C2A4E1-2D5E-4F4A-9F1C-HUBPLAY00001}
AppName=HubPlay
AppVersion={#AppVersion}
AppVerName=HubPlay {#AppVersion}
AppPublisher=HubPlay
AppPublisherURL=https://github.com/Alexzafra13/HubPlay_demo
AppSupportURL=https://github.com/Alexzafra13/HubPlay_demo/issues
DefaultDirName={autopf}\HubPlay
DefaultGroupName=HubPlay
DisableProgramGroupPage=yes
OutputDir={#OutputDir}
OutputBaseFilename=HubPlay-Setup-{#AppVersion}-windows-amd64
Compression=lzma
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; Solicitar admin: necesitamos escribir a Program Files + registrar servicio.
PrivilegesRequired=admin
UninstallDisplayIcon={app}\hubplay.ico
; Mostrar versión en Add/Remove Programs sin la "v" inicial si la tiene.
VersionInfoVersion=0.0.0.0
VersionInfoProductName=HubPlay
VersionInfoCompany=HubPlay

[Languages]
Name: "spanish"; MessagesFile: "compiler:Languages\Spanish.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce
Name: "service"; Description: "Instalar HubPlay como servicio de Windows (recomendado — arranca con el PC, sin consola visible)"; GroupDescription: "Modo de ejecución:"; Flags: checkedonce
Name: "openbrowser"; Description: "Abrir HubPlay al terminar"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce

[Files]
Source: "{#SourceDir}\hubplay.exe";              DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\ffmpeg.exe";               DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\ffprobe.exe";              DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\hubplay.example.yaml";     DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\LICENSE";                  DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "{#SourceDir}\LICENSE-ffmpeg.txt";       DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist
Source: "hubplay.ico";                           DestDir: "{app}"; Flags: ignoreversion
Source: "launch-hubplay.vbs";                    DestDir: "{app}"; Flags: ignoreversion
; NSSM — service manager. El workflow lo descarga a scripts/vendor/nssm.exe
; antes de invocar ISCC.
Source: "vendor\nssm.exe";                       DestDir: "{app}"; Flags: ignoreversion; Tasks: service

[Icons]
; Lanzador principal: VBS que intenta Edge/Chrome --app (modo PWA standalone,
; ventana sin barra ni tabs) y cae al navegador por defecto si no.
Name: "{group}\HubPlay";                        Filename: "wscript.exe"; Parameters: """{app}\launch-hubplay.vbs"""; IconFilename: "{app}\hubplay.ico"; WorkingDir: "{app}"
Name: "{group}\HubPlay (arrancar manual)";      Filename: "{app}\hubplay.exe"; Parameters: "--config ""{app}\hubplay.yaml"""; WorkingDir: "{app}"; IconFilename: "{app}\hubplay.ico"
Name: "{group}\Desinstalar HubPlay";            Filename: "{uninstallexe}"
Name: "{autodesktop}\HubPlay";                  Filename: "wscript.exe"; Parameters: """{app}\launch-hubplay.vbs"""; IconFilename: "{app}\hubplay.ico"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
; Copiar el yaml de ejemplo a hubplay.yaml si no existe (primer install).
; Sin /Y para no sobrescribir un yaml editado por el operador en upgrades.
Filename: "cmd.exe"; Parameters: "/C if not exist ""{app}\hubplay.yaml"" copy /Y ""{app}\hubplay.example.yaml"" ""{app}\hubplay.yaml"""; Flags: runhidden waituntilterminated; Description: "Crear hubplay.yaml inicial"

; Modo servicio: instalar+arrancar service "HubPlay".
Filename: "{app}\nssm.exe"; Parameters: "install HubPlay ""{app}\hubplay.exe"" --config ""{app}\hubplay.yaml"""; Flags: runhidden waituntilterminated; Tasks: service
Filename: "{app}\nssm.exe"; Parameters: "set HubPlay AppDirectory ""{app}"""; Flags: runhidden waituntilterminated; Tasks: service
Filename: "{app}\nssm.exe"; Parameters: "set HubPlay DisplayName ""HubPlay Media Server"""; Flags: runhidden waituntilterminated; Tasks: service
Filename: "{app}\nssm.exe"; Parameters: "set HubPlay Description ""Servidor de media self-hosted estilo Plex/Jellyfin"""; Flags: runhidden waituntilterminated; Tasks: service
Filename: "{app}\nssm.exe"; Parameters: "set HubPlay Start SERVICE_AUTO_START"; Flags: runhidden waituntilterminated; Tasks: service
Filename: "{app}\nssm.exe"; Parameters: "start HubPlay"; Flags: runhidden waituntilterminated; Tasks: service

; Abrir el navegador al final si el usuario marcó la opción. Espera
; 3 segundos para dar tiempo al servicio a levantarse y aceptar conexiones.
; Pequeño delay para que el servicio acepte conexiones, luego abre como app.
Filename: "cmd.exe"; Parameters: "/C timeout /T 3 /NOBREAK > nul && wscript ""{app}\launch-hubplay.vbs"""; Flags: runhidden nowait; Tasks: openbrowser

[UninstallRun]
; Parar y desinstalar el servicio antes de borrar los archivos. ||
; opcional con error en caso de que el servicio no exista (instalación
; sin el task service).
Filename: "{app}\nssm.exe"; Parameters: "stop HubPlay";    Flags: runhidden waituntilterminated; RunOnceId: "StopHubPlayService"
Filename: "{app}\nssm.exe"; Parameters: "remove HubPlay confirm"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveHubPlayService"

[UninstallDelete]
; hubplay.yaml editado por el operador + sqlite generado en {app}\data
; — preguntar al final. Como Inno no tiene "ask on uninstall" sencillo,
; dejamos los datos para que el operador los borre a mano si quiere.
; El uninstaller borra sólo lo que él instaló (los Source: arriba).
Type: files; Name: "{app}\hubplay.example.yaml"

[Code]
// Mensaje final en la última página del wizard, en modo servicio:
// recordar al usuario que la consola ya no aparece — eso es bueno.
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and IsTaskSelected('service') then begin
    Log('HubPlay instalado como servicio. Inicia automáticamente al boot.');
  end;
end;
