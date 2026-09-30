$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Dist = Join-Path $Root 'dist'
New-Item -ItemType Directory -Force -Path $Dist | Out-Null

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go bulunamadı. Go kurulumu sonrası PowerShell yeniden açılmalı.'
}

Push-Location $Root
try {
    Write-Host 'Building NexDesk.exe ...'
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    # -H=windowsgui: GUI alt-sistemi olarak derle; açılışta konsol (DOS) penceresi çıkmasın.
    go build -trimpath -ldflags='-s -w -H=windowsgui' -o (Join-Path $Dist 'NexDesk.exe') './cmd/remotesupport'
    if ($LASTEXITCODE -ne 0) { throw 'NexDesk.exe build failed.' }

    Write-Host 'Signaling embedded in NexDesk.exe.'

    Write-Host ''
    Write-Host 'BUILD OK'
    Write-Host (Join-Path $Dist 'NexDesk.exe')
}
finally { Pop-Location }
