# NexDesk sürüm hazırlama: derle + imzala + sunucu ikilisi.
#   powershell -ExecutionPolicy Bypass -File release.ps1            (yalnızca hazırlar)
#   powershell -ExecutionPolicy Bypass -File release.ps1 -Publish   (gh CLI varsa GitHub sürümünü de oluşturur)
# İlk kez: go run ./cmd/nd-sign keygen -pub cmd/remotesupport/assets/update-pubkey.txt
param([switch]$Publish, [string]$Notes = '')
$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $Root
try {
    $m = Select-String -Path 'cmd\remotesupport\branding.go' -Pattern 'const appVersion = "([^"]+)"'
    if (-not $m) { throw 'appVersion bulunamadı (branding.go).' }
    $Ver = $m.Matches[0].Groups[1].Value
    $Pub = (Get-Content 'cmd\remotesupport\assets\update-pubkey.txt' -Raw -ErrorAction SilentlyContinue)
    if (-not $Pub -or -not $Pub.Trim()) { throw 'Güncelleme anahtarı yok. Önce: go run ./cmd/nd-sign keygen -pub cmd/remotesupport/assets/update-pubkey.txt' }
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

    if ($Publish) {
        if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { throw 'gh CLI bulunamadı; dosyaları GitHub > Releases üzerinden elle yükleyin.' }
        $args = @('release', 'create', "v$Ver", 'dist\NexDesk.exe', 'dist\NexDesk.exe.sig', '--repo', 'Eirahoutmoss/remotesupport', '--title', "NexDesk $Ver")
        if ($Notes) { $args += @('--notes', $Notes) } else { $args += '--generate-notes' }
        gh @args
        if ($LASTEXITCODE -ne 0) { throw 'GitHub sürümü oluşturulamadı.' }
    }
    Write-Host ''
    Write-Host "HAZIR: dist\NexDesk.exe + dist\NexDesk.exe.sig (v$Ver), sunucu\nexdesk-signaling"
}
finally { Pop-Location }
