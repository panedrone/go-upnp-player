# Build: frontend (Vite) -> web/dist, then a single exe with the frontend embedded.
# -H=windowsgui: no console window; shut down via the Quit button on the page,
# the log is written to %LocalAppData%\upnp-player\upnp-player.log.
$ErrorActionPreference = 'Stop'
Push-Location "$PSScriptRoot\web"
try {
    if (-not (Test-Path node_modules)) { npm ci }
    npm run build
} finally { Pop-Location }
Push-Location $PSScriptRoot
try {
    go build -ldflags "-s -w -H=windowsgui" -o bin\upnp-player.exe .\cmd\player
} finally { Pop-Location }
Write-Host "bin\upnp-player.exe"
