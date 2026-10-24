# NexDesk sürüm hazırlama: derle + imzala + sunucu ikilisi.
#   powershell -ExecutionPolicy Bypass -File release.ps1            (yalnızca hazırlar; Android dahil)
#   ... -NoAndroid                                                  (APK olmadan)
#   powershell -ExecutionPolicy Bypass -File release.ps1 -Publish   (gh CLI varsa GitHub sürümünü de oluşturur)
# İlk kez: go run ./cmd/nd-sign keygen -pub shared/update/update-pubkey.txt
param([switch]$Publish, [switch]$NoAndroid, [string]$Notes = '')
$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $Root
try {
    $m = Select-String -Path 'cmd\remotesupport\branding.go' -Pattern 'const appVersion = "([^"]+)"'
    if (-not $m) { throw 'appVersion bulunamadı (branding.go).' }
    $Ver = $m.Matches[0].Groups[1].Value
    $Pub = (Get-Content 'shared\update\update-pubkey.txt' -Raw -ErrorAction SilentlyContinue)
    if (-not $Pub -or -not $Pub.Trim()) { throw 'Güncelleme anahtarı yok. Önce: go run ./cmd/nd-sign keygen -pub shared/update/update-pubkey.txt' }
    Write-Host "== Sürüm $Ver"

    & (Join-Path $Root 'build.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'Derleme başarısız.' }

    Write-Host '== İmzalanıyor'
    $env:GOOS = ''; $env:GOARCH = ''
    go run ./cmd/nd-sign sign -version $Ver dist\NexDesk.exe
    if ($LASTEXITCODE -ne 0) { throw 'İmzalama başarısız.' }

    Write-Host '== Sunucu ikilisi (linux/amd64)'
    $env:CGO_ENABLED = '0'; $env:GOOS = 'linux'; $env:GOARCH = 'amd64'
    go build -trimpath -ldflags='-s -w' -o sunucu\nexdesk-signaling ./cmd/signaling
    if ($LASTEXITCODE -ne 0) { throw 'Sunucu derlemesi başarısız.' }
    $env:GOOS = ''; $env:GOARCH = ''

    $Assets = @('dist\NexDesk.exe', 'dist\NexDesk.exe.sig', 'sunucu\nexdesk-signaling')
    if (-not $NoAndroid) {
        & (Join-Path $Root 'build-apk.ps1')
        if ($LASTEXITCODE -ne 0) { throw 'Android derlemesi başarısız.' }
        $Assets += @('dist\NexDesk.apk', 'dist\NexDesk.apk.sig')
    }

    if ($Publish) {
        if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw 'gh CLI bulunamadı; dosyaları GitHub > Releases üzerinden elle yükleyin.' }
        $args = @('release', 'create', "v$Ver") + $Assets + @('--repo', 'Eirahoutmoss/NexDesk', '--title', "NexDesk $Ver")
        if ($Notes) { $args += @('--notes', $Notes) } else { $args += '--generate-notes' }
        gh @args
        if ($LASTEXITCODE -ne 0) { throw 'GitHub sürümü oluşturulamadı.' }
    }
    Write-Host ''
    Write-Host "HAZIR (v$Ver) — GitHub sürümüne yüklenecek dosyalar:"
    $Assets | ForEach-Object { Write-Host "   $_" }
}
finally { Pop-Location }
