# Build the exe. The UI bundle (web/dist) is built at design time and committed,
# so a plain build needs Go only; pass -UI to rebuild the bundle first (needs
# Node), then commit web/dist together with the UI change.
# -H=windowsgui: no console window; shut down via the Quit button on the page,
# the log is written to %LocalAppData%\go-upnp-player\go-upnp-player.log.
param([switch]$UI)
$ErrorActionPreference = 'Stop'
if ($UI) {
    Push-Location "$PSScriptRoot\web"
    try {
        if (-not (Test-Path node_modules)) { npm ci }
        npm run build
    } finally { Pop-Location }
}
Push-Location $PSScriptRoot
try {
    go build -ldflags "-s -w -H=windowsgui" -o bin\go-upnp-player.exe .\cmd\go-upnp-player
} finally { Pop-Location }
Write-Host "bin\go-upnp-player.exe"
