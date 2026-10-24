# NexDesk Android (teknisyen uygulaması) derlemesi:
#   powershell -ExecutionPolicy Bypass -File build-apk.ps1
# Çıktı: dist\NexDesk.apk + dist\NexDesk.apk.sig (güncelleme imzası)
# Gerekenler: Go, Android Studio (SDK + NDK + JBR). gomobile yoksa kurulur.
$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $Root
try {
    $m = Select-String -Path 'cmd\remotesupport\branding.go' -Pattern 'const appVersion = "([^"]+)"'
    $Ver = $m.Matches[0].Groups[1].Value
    Write-Host "== NexDesk Android $Ver"

    # --- Android SDK / NDK / Java
    $Sdk = $env:ANDROID_HOME
    if (-not $Sdk) { $Sdk = $env:ANDROID_SDK_ROOT }
    if (-not $Sdk) { $Sdk = Join-Path $env:LOCALAPPDATA 'Android\Sdk' }
    if (-not (Test-Path $Sdk)) { throw "Android SDK bulunamadı ($Sdk). ANDROID_HOME ayarlayın." }
    $env:ANDROID_HOME = $Sdk
    if (-not $env:ANDROID_NDK_HOME) {
        $ndk = Get-ChildItem (Join-Path $Sdk 'ndk') -Directory -ErrorAction SilentlyContinue | Sort-Object Name -Descending | Select-Object -First 1
        if (-not $ndk) { throw 'Android NDK yok. Android Studio > SDK Manager > SDK Tools > "NDK (Side by side)" kurun.' }
        $env:ANDROID_NDK_HOME = $ndk.FullName
    }
    # Java 17+: JAVA_HOME, Android Studio'nun JBR'si, Adoptium/Microsoft JDK ya da PATH'teki java
    if (-not ($env:JAVA_HOME -and (Test-Path (Join-Path $env:JAVA_HOME 'bin\java.exe')))) {
        $env:JAVA_HOME = $null
        $cands = @(
            "$env:ProgramFiles\Android\Android Studio*\jbr",
            "$env:LOCALAPPDATA\Programs\Android Studio*\jbr",
            "$env:LOCALAPPDATA\JetBrains\Toolbox\apps\AndroidStudio\*\*\jbr",
            "$env:LOCALAPPDATA\JetBrains\Toolbox\apps\android-studio*\jbr",
            "$env:ProgramFiles\Eclipse Adoptium\jdk-*",
            "$env:ProgramFiles\Microsoft\jdk-*",
            "$env:ProgramFiles\Java\jdk-*"
        )
        $j = $cands | ForEach-Object { Get-Item $_ -ErrorAction SilentlyContinue } |
            Where-Object { Test-Path (Join-Path $_ 'bin\java.exe') } | Select-Object -First 1
        if (-not $j) {
            $c = Get-Command java -ErrorAction SilentlyContinue
            if ($c) { $j = Split-Path (Split-Path $c.Source) }
        }
        if ($j) { $env:JAVA_HOME = "$j" }
    }
    if (-not $env:JAVA_HOME) { throw 'Java 17 bulunamadı. JAVA_HOME ayarlayın.' }
    $env:PATH = (Join-Path $env:JAVA_HOME 'bin') + ';' + $env:PATH
    Write-Host "SDK: $Sdk"; Write-Host "NDK: $env:ANDROID_NDK_HOME"; Write-Host "Java: $env:JAVA_HOME"

    # --- gomobile
    $gopath = (go env GOPATH)
    $env:PATH = (Join-Path $gopath 'bin') + ';' + $env:PATH
    if (-not (Get-Command gomobile -ErrorAction SilentlyContinue)) {
        Write-Host '== gomobile kuruluyor'
        go install golang.org/x/mobile/cmd/gomobile@latest
        go install golang.org/x/mobile/cmd/gobind@latest
        if ($LASTEXITCODE -ne 0) { throw 'gomobile kurulamadı.' }
    }
    gomobile init
    if ($LASTEXITCODE -ne 0) { throw 'gomobile init başarısız.' }
    go get golang.org/x/mobile/bind@latest
    if ($LASTEXITCODE -ne 0) { throw 'golang.org/x/mobile eklenemedi.' }

    # --- built-in server settings (git-ignored, holds the TURN password)
    $local = 'cmd\remotesupport\assets\server-defaults.local.json'
    if (Test-Path $local) { Copy-Item $local 'mobile\assets\server-defaults.local.json' -Force }
    else { Write-Warning 'server-defaults.local.json yok: uygulamada sunucu adresi Ayarlar''dan girilmeli.' }

    # --- Go core -> AAR
    Write-Host '== Go çekirdeği derleniyor (gomobile bind)'
    New-Item -ItemType Directory -Force -Path 'android\app\libs' | Out-Null
    $env:GOOS = ''; $env:GOARCH = ''; $env:CGO_ENABLED = ''
    gomobile bind -target 'android/arm64,android/arm,android/amd64' -androidapi 24 -javapkg com.eirahoutmoss -ldflags '-s -w' -o 'android\app\libs\nexdesk.aar' ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'gomobile bind başarısız.' }

    # --- release keystore (same key for every release, or updates fail)
    $nd = Join-Path $env:USERPROFILE '.nexdesk'
    New-Item -ItemType Directory -Force -Path $nd | Out-Null
    $ks = Join-Path $nd 'android-release.jks'
    $kp = Join-Path $nd 'keystore.properties'
    if (-not (Test-Path $ks)) {
        Write-Host '== İmza anahtar deposu oluşturuluyor (bir kez)'
        $pw = ([guid]::NewGuid().ToString('N') + [guid]::NewGuid().ToString('N')).Substring(0, 32)
        & keytool -genkeypair -v -keystore $ks -storetype PKCS12 -alias nexdesk -keyalg RSA -keysize 4096 -validity 36500 `
            -storepass $pw -keypass $pw -dname 'CN=NexDesk, O=Eirahoutmoss, C=TR'
        if ($LASTEXITCODE -ne 0) { throw 'keytool başarısız.' }
        @("storeFile=$($ks -replace '\\','/')", "storePassword=$pw", 'keyAlias=nexdesk', "keyPassword=$pw") | Set-Content -Encoding ASCII $kp
        Write-Warning "YEDEKLEYİN: $ks ve $kp — kaybolursa kurulu uygulamalar güncellenemez."
    }

    # --- APK
    $lp = 'android\local.properties'
    if (-not (Test-Path $lp)) { "sdk.dir=$($Sdk -replace '\\','/')" | Set-Content -Encoding ASCII $lp }
    Write-Host '== APK derleniyor (Gradle)'
    Push-Location android
    try {
        & .\gradlew.bat --no-daemon assembleRelease
        if ($LASTEXITCODE -ne 0) { throw 'Gradle derlemesi başarısız.' }
    } finally { Pop-Location }
    New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
    Copy-Item 'android\app\build\outputs\apk\release\app-release.apk' 'dist\NexDesk.apk' -Force

    Write-Host '== Güncelleme imzası'
    go run ./cmd/nd-sign sign -version $Ver dist\NexDesk.apk
    if ($LASTEXITCODE -ne 0) { throw 'APK imzalanamadı (nd-sign).' }
    Write-Host ''
    Write-Host "APK HAZIR: dist\NexDesk.apk (v$Ver)"
}
finally { Pop-Location }
