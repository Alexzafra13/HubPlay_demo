' Lanzador de HubPlay. Intenta abrir como app standalone (Edge/Chrome
' en modo --app, ventana sin barra ni tabs). Si ninguno está, fallback
' al navegador por defecto.

Dim sh
Set sh = CreateObject("WScript.Shell")
Dim url
url = "http://localhost:8096"

On Error Resume Next

' Edge (preinstalado en Windows 10/11).
sh.Run "msedge.exe --app=" & url, 0, False
If Err.Number = 0 Then WScript.Quit
Err.Clear

' Chrome como segunda opción.
sh.Run "chrome.exe --app=" & url, 0, False
If Err.Number = 0 Then WScript.Quit
Err.Clear

' Fallback: navegador por defecto, ventana normal.
sh.Run url, 1, False
