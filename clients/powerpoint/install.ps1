<#
.SYNOPSIS
  magi PowerPoint 플러그인을 이 계정에 설치한다 — 빌드, 인증서, 애드인 등록, 헬퍼 기동까지 한 번에.

.DESCRIPTION
  저장소 루트에서(또는 어디서든) 이 파일을 돌리면:
    1. Office 판을 읽어(Microsoft 365 인가 볼륨 판/LTSC 인가) 길을 고른다.
    2. magi.exe 를 빌드해 설치 폴더에 놓고(헬퍼는 `magi office` 안 — 엑셀·워드와 공용, clients/office/install.ps1 이 셋을 한 번에 깐다),
       애드인 파일을 그 옆 clients\powerpoint\addin 에 복사한다.
       볼륨 판이면 COM 손(magi-ppt-hand)도 빌드한다(.NET SDK 가 있을 때).
    3. 데몬 권한 모드를 allow 로 둔다(~/.magi/config.toml). 사용자 결정(2026-09-05·06).
    4. 헬퍼를 띄우고, 헬퍼가 만든 인증서를 이 계정의 신뢰 저장소에 넣는다(Windows 가 한 번 묻는다).
    5. 애드인을 등록한다 — M365 는 개발자 키, 볼륨 판은 신뢰 카탈로그(공유 폴더).
    6. 로그인할 때 헬퍼(와 볼륨 판이면 손 감시기)가 같이 뜨게 한다. -NoAutostart 로 끌 수 있다.

  다시 돌려도 된다 — 이미 된 것은 건너뛴다. 사유는 docs/INSTALL.ko.md.

  ⚠ PowerShell 5.1 은 BOM 없는 UTF-8 을 ANSI 로 읽는다. 이 파일은 BOM 이 있어야 한다(tools/README.md).

.PARAMETER Dest
  설치 폴더. 기본 %LOCALAPPDATA%\magi\ppt

.PARAMETER NoAutostart
  로그인 때 자동으로 띄우는 등록(HKCU\...\Run)을 안 한다.

.PARAMETER SkipBuild
  빌드를 건너뛴다 — Dest 에 이미 실행 파일이 있을 때(배포본).

.PARAMETER Clean
  애드인을 **지우고 다시 깐다.** 등록(신뢰 카탈로그 키·개발자 키)을 빼고 Office 의 애드인 캐시(Wef 폴더)를 비운 뒤
  보통 설치를 이어 간다. 이름·아이콘이 바뀌었는데 리본이 옛것을 그릴 때 — 캐시가 매니페스트 사본을 들고 있어서다.
  PowerPoint 가 떠 있으면 멈춘다(캐시를 쥐고 있다).
#>
[CmdletBinding()]
param(
  [string]$Dest = (Join-Path $env:LOCALAPPDATA 'magi\ppt'),
  [switch]$NoAutostart,
  [switch]$SkipBuild,
  [switch]$Clean,
  [string]$CatalogUnc = ''
)
# -CatalogUnc: 카탈로그 폴더(~/.magi/ppt-catalog)가 보이는 **진짜 공유**의 UNC. 비우면 이 계정의 공유 목록에서
# 그 폴더를 덮는 공유를 찾고, 없으면 관리 공유(\\<컴퓨터>\C$\…)를 쓴다. PowerPoint 2021 은 관리 공유도 받지만,
# 같은 머신의 Excel 2021 은 관리 공유 형태의 카탈로그를 **켤 때마다 지운다**(2026-09-06 실측) — 그러면 이 등록도
# 같이 사라진다. 진짜 공유가 안전하다: New-SmbShare -Name magi -Path "$env:USERPROFILE\.magi" -ReadAccess $env:USERNAME

$ErrorActionPreference = 'Stop'
$repo = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$addinSrc = Join-Path $PSScriptRoot 'addin'
# 설정 디렉토리는 **~/.magi 로 못 박는다.** 헬퍼의 기본값은 %APPDATA%\magi 인데, 이 머신에서 그 아래에는
# AF_UNIX 소켓이 안 선다(MANUAL §2.5) — 헬퍼가 띄운 데몬에 아무도 못 붙는다. 그리고 첫 설치 시험(2026-09-06)에서
# 헬퍼가 거기에 **새 인증서**를 만들어, 신뢰 저장소에 든 것과 다른 인증서로 떠 있었다.
$configDir = if ($env:MAGI_CONFIG_DIR) { $env:MAGI_CONFIG_DIR } else { Join-Path $env:USERPROFILE '.magi' }
$helperUrl = 'https://127.0.0.1:3000/ppt'   # magi office 의 파워포인트 몫
# PowerShell 5.1 은 TLS 1.0 으로 말을 건다 — Go 서버는 1.2 이상만 받는다. 이것이 없으면 「기본 연결이 닫혔습니다」.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
[Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }

function Say($s) { Write-Host "▸ $s" }
function Done($s) { Write-Host "  ✓ $s" -ForegroundColor Green }
function Warn($s) { Write-Host "  ⚠ $s" -ForegroundColor Yellow }
function Fail($s) { Write-Host "  ✗ $s" -ForegroundColor Red; exit 1 }

# ── 0. 어느 Office 인가 ───────────────────────────────────────────────────────
Say 'Office 판을 읽는다'
$c2r = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Office\ClickToRun\Configuration' -ErrorAction SilentlyContinue
if (-not $c2r) { Fail 'Office(클릭 투 런)가 안 보인다. PowerPoint 2021 이상 또는 Microsoft 365 가 필요하다.' }
$ids = "$($c2r.ProductReleaseIds)"
$perpetual = ($ids -match 'Volume|2019|2021|2024')      # 볼륨 판/LTSC — 개발자 키를 무시한다(INSTALL.ko.md §3.2)
$edition = if ($perpetual) { "볼륨 판 $($c2r.VersionToReport)" } else { "Microsoft 365 $($c2r.VersionToReport)" }
Done "$edition ($ids)"
if ($perpetual) { Warn '이 판의 작업창은 편집을 못 한다(PowerPointApi 1.2) — 편집은 COM 손이 맡는다. 같이 설치한다.' }

# ── 1. 도구 ─────────────────────────────────────────────────────────────────
Say '필요한 도구를 본다'
$go = Get-Command go -ErrorAction SilentlyContinue
$dotnet = Get-Command dotnet -ErrorAction SilentlyContinue
if (-not $dotnet -and (Test-Path 'C:\Program Files\dotnet\dotnet.exe')) { $dotnet = Get-Item 'C:\Program Files\dotnet\dotnet.exe' }
if (-not $SkipBuild -and -not $go) { Fail 'Go 가 없다(go.dev/dl). 빌드된 실행 파일이 이미 있으면 -SkipBuild.' }
if ($go) { Done "go: $($go.Source)" }
if ($perpetual) {
  if ($dotnet) { Done "dotnet: $(if ($dotnet.Source) { $dotnet.Source } else { $dotnet.FullName })" }
  else { Warn '.NET SDK 가 없다 — COM 손을 못 만든다. dotnet.microsoft.com 에서 .NET 9 SDK 를 깔고 다시 돌려라.' }
}
$ollama = Get-Command ollama -ErrorAction SilentlyContinue
if ($ollama) { Done "ollama: $($ollama.Source)" } else { Warn 'ollama 가 없다. 기본 모델은 Ollama 클라우드(gpt-oss:120b-cloud)라 ollama 를 깔고 `ollama signin` 을 한 번 해야 한다. 다른 백엔드를 쓰면 ~/.magi/config.toml 의 model/base_url.' }

# ── 2. 전에 깔린 것을 멈춘다(설치 폴더의 실행 파일만) ─────────────────────────
Say '설치 폴더의 옛 프로세스를 멈춘다'
New-Item -ItemType Directory -Force $Dest | Out-Null
$destFull = (Resolve-Path $Dest).Path
# 손 감시기부터 멈춘다 — 안 멈추면 아래서 손을 멈추자마자 감시기가 다시 띄워 COM 손 빌드가 파일 잠금으로 죽는다
# (2026-09-06 실측). 끝에서 다시 띄운다.
Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.ProcessId -ne $PID -and $_.CommandLine -like '*-File*' -and $_.CommandLine -like '*hand-watch.ps1*' } |
  ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; Done "손 감시기 (pid $($_.ProcessId)) 멈춤" }
# 데몬(magi.exe)도 설치 폴더의 것이면 멈춘다 — 헬퍼가 거기서 띄운 것이고, 안 멈추면 새 magi.exe 를 못 쓴다.
# 하던 대화는 끊긴다. 창을 다시 열면 헬퍼가 새 데몬을 띄운다.
foreach ($name in @('magi-ppt', 'magi-ppt-hand', 'magi')) {   # magi-ppt 는 옛 헬퍼 — 남아 있으면 3000 을 쥔다
  Get-Process $name -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith($destFull, 'OrdinalIgnoreCase') } |
    ForEach-Object { Stop-Process -Id $_.Id -Force; $_.WaitForExit(5000) | Out-Null; Done "$name (pid $($_.Id)) 멈춤" }
}
# 헬퍼는 사용자당 하나다 — 다른 자리에서 뜬 것이 3000 번을 쥐고 있으면 새것은 물러난다.
Start-Sleep -Milliseconds 500
$other = Get-Process magi-ppt -ErrorAction SilentlyContinue | Where-Object { -not $_.HasExited }
if ($other) { $other | Stop-Process -Force; Done '옛 헬퍼 magi-ppt 를 멈췄다 — 이제 magi office 하나다' }
Remove-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' 'magi-ppt' -ErrorAction SilentlyContinue

# ── 3. 빌드·복사 ─────────────────────────────────────────────────────────────
if (-not $SkipBuild) {
  Say 'magi 와 헬퍼를 빌드한다'
  Push-Location $repo
  try {
    & go build -o (Join-Path $Dest 'magi.exe') ./cmd/magi
    if ($LASTEXITCODE -ne 0) { Fail 'magi 빌드 실패' }
  } finally { Pop-Location }
  Done "magi.exe → $Dest (헬퍼는 magi office)"
  if ($perpetual -and $dotnet) {
    Say 'COM 손을 빌드한다'
    $dn = if ($dotnet.Source) { $dotnet.Source } else { $dotnet.FullName }
    $handProj = Join-Path $PSScriptRoot 'hand-com\src\magi-ppt-hand.csproj'
    $handOut = Join-Path $Dest 'hand'
    & $dn build $handProj -c Release -o $handOut --nologo -v q
    if ($LASTEXITCODE -ne 0) { Fail 'COM 손 빌드 실패' }
    Done "magi-ppt-hand.exe → $handOut"
  }
} else { Say '빌드는 건너뛴다(-SkipBuild)' }
if (-not (Test-Path (Join-Path $Dest 'magi.exe'))) { Fail "$Dest\magi.exe 가 없다" }

Say '애드인 파일을 헬퍼 옆에 놓는다'
$addinDest = Join-Path $Dest 'clients\powerpoint\addin'   # magi office 가 자기 옆 clients\<앱>\addin 을 본다
& robocopy $addinSrc $addinDest /MIR /NFL /NDL /NJH /NJS /NP | Out-Null   # robocopy 는 0~7 이 성공
if ($LASTEXITCODE -ge 8) { Fail "애드인 복사 실패(robocopy $LASTEXITCODE)" }
Done "addin → $addinDest"
Copy-Item (Join-Path $PSScriptRoot 'hand-watch.ps1') (Join-Path $Dest 'hand-watch.ps1') -Force

# ── 4. 데몬 권한 모드 = allow ────────────────────────────────────────────────
Say '데몬 권한 모드를 allow 로 둔다'
New-Item -ItemType Directory -Force $configDir | Out-Null
$cfg = Join-Path $configDir 'config.toml'
$lines = @(); if (Test-Path $cfg) { $lines = @(Get-Content $cfg -Encoding UTF8) }
$live = $lines | Where-Object { $_ -match '^\s*permission\s*=' }
if ($live) {
  if ($live -notmatch '"allow"') {
    $lines = $lines | ForEach-Object { if ($_ -match '^\s*permission\s*=') { 'permission = "allow"   # magi 플러그인 설치기가 바꿨다 — 사용자 결정' } else { $_ } }
    [IO.File]::WriteAllLines($cfg, $lines, (New-Object Text.UTF8Encoding $false)); Done "config.toml: permission 을 allow 로 바꿨다"
  } else { Done 'config.toml: 이미 allow' }
} else {
  $lines += ''; $lines += '# magi PowerPoint 플러그인 설치기가 더했다(사용자 결정: 승인 창이 흐름을 끊는 품이 더 크다)'; $lines += 'permission = "allow"'
  [IO.File]::WriteAllLines($cfg, $lines, (New-Object Text.UTF8Encoding $false)); Done 'config.toml: permission = "allow" 를 더했다'
}

# ── 5. 헬퍼를 띄우고 인증서를 넣는다 ─────────────────────────────────────────
Say '헬퍼를 띄운다'
$helperExe = Join-Path $Dest 'magi.exe'
$helperArgs = @('office', '-config-dir', $configDir)
if (-not (Get-NetTCPConnection -LocalPort 3000 -State Listen -ErrorAction SilentlyContinue)) {
  Start-Process -FilePath $helperExe -ArgumentList $helperArgs -WorkingDirectory $Dest -WindowStyle Hidden | Out-Null
}
$pem = Join-Path $configDir 'office-helper-cert.pem'
$deadline = (Get-Date).AddSeconds(20)
while (-not (Test-Path $pem) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
if (-not (Test-Path $pem)) { Fail "헬퍼가 인증서를 안 만들었다($pem). 헬퍼 로그를 봐라." }
# 살아 있는지는 curl.exe(Windows 10 부터 기본)로 묻고, 없으면 TCP 로 묻는다. Invoke-WebRequest 는 안 쓴다 —
# PowerShell 5.1 의 TLS 클라이언트가 이 Go 서버와 악수를 못 해 「기본 연결이 닫혔습니다」만 낸다(2026-09-06 실측).
$curl = Get-Command curl.exe -ErrorAction SilentlyContinue
$deadline = (Get-Date).AddSeconds(20); $up = $false
while (-not $up -and (Get-Date) -lt $deadline) {
  if ($curl) {
    $code = & $curl.Source -sk -o NUL -w '%{http_code}' --max-time 3 "$helperUrl/taskpane.html" 2>$null
    if ("$code" -eq '200') { $up = $true }
  } else {
    $tcp = New-Object Net.Sockets.TcpClient
    try { $tcp.Connect('127.0.0.1', 3000); $up = $true } catch { } finally { $tcp.Close() }
  }
  if (-not $up) { Start-Sleep -Milliseconds 700 }
}
if ($up) { Done "헬퍼가 $helperUrl 에 떠 있다" }
else { Fail "헬퍼가 $helperUrl 에 안 답한다. 다른 것이 3000 번을 쥐고 있으면 그것을 끄고 다시: Stop-Process -Name magi,magi-ppt" }

Say '인증서를 이 계정의 신뢰 저장소에 넣는다'
$cert = New-Object Security.Cryptography.X509Certificates.X509Certificate2 $pem
$have = Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Thumbprint -eq $cert.Thumbprint }
if ($have) { Done "이미 있다($($cert.Thumbprint))" }
else {
  Write-Host '  Windows 가 「루트 인증서를 설치하시겠습니까」라고 묻는다 — 예를 누른다. (127.0.0.1 하나에만 유효한 자기 서명 인증서다)' -ForegroundColor Cyan
  Import-Certificate -FilePath $pem -CertStoreLocation Cert:\CurrentUser\Root | Out-Null
  $have = Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Thumbprint -eq $cert.Thumbprint }
  if ($have) { Done '넣었다' } else { Fail '인증서가 안 들어갔다(취소했나?). 다시 돌리면 이 단계만 다시 한다.' }
}

# ── 6. 애드인 등록 ───────────────────────────────────────────────────────────
$wef = 'HKCU:\Software\Microsoft\Office\16.0\WEF'
# ── 지우기(-Clean) — 등록과 캐시를 걷어 낸 뒤 아래 보통 설치가 다시 넣는다 ───────────────────────────
# MS 문서: 낱개 매니페스트만 지우지 말고 Wef 폴더를 통째로 비워라(낱개만 지우면 전부 안 뜬다). 리본 캐시 만료값도
# 같이 지운다. PowerPoint 가 떠 있으면 캐시를 쥐고 있어 되살아난다 — 끄라고 하고 멈춘다.
if ($Clean) {
  Say '애드인 등록과 캐시를 지운다(-Clean)'
  if (Get-Process POWERPNT -ErrorAction SilentlyContinue) { Fail 'PowerPoint 가 떠 있다 — 끄고 다시 돌려라(캐시를 쥐고 있다)' }
  foreach ($k in @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)) {
    $u = (Get-ItemProperty $k.PSPath).Url
    if ($u -and $u -match '\\\.magi\\(catalog|ppt-catalog|xl-catalog)$') { Remove-Item $k.PSPath -Recurse -Force; Done "카탈로그 키 $($k.PSChildName) 삭제 ($u) — 엑셀 판과 같이 쓰는 키다, 그쪽도 다시 추가해야 한다" }
  }
  if (Get-ItemProperty "$wef\Developer" -Name magi -ErrorAction SilentlyContinue) { Remove-ItemProperty "$wef\Developer" -Name magi; Done 'Developer\magi 삭제' }
  foreach ($v in ((Get-Item $wef -ErrorAction SilentlyContinue).Property | Where-Object { $_ -like 'PowerPoint_*RibbonCustomizationExpire' })) {
    Remove-ItemProperty -Path $wef -Name $v -ErrorAction SilentlyContinue
  }
  $cache = Join-Path $env:LOCALAPPDATA 'Microsoft\Office\16.0\Wef'
  if (Test-Path $cache) { Get-ChildItem $cache -Force | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue; Done "캐시 비움 $cache" }
  Warn '다시 넣은 뒤 PowerPoint 를 켜고 삽입 → 내 추가 기능 → 공유 폴더에서 「Magi」를 다시 추가해야 리본에 선다'
}
$manifest = Join-Path $addinDest 'manifest.xml'
if ($perpetual) {
  Say '애드인을 신뢰 카탈로그로 등록한다(볼륨 판은 개발자 키를 무시한다)'
  # 카탈로그는 설정 디렉토리 옆에 둔다(~/.magi/ppt-catalog). 첫 시험(2026-09-06)에서 %LOCALAPPDATA% 아래는
  # 관리 공유(\\localhost\C$\…)로 닿지 않았고, 프로필 바로 아래는 닿았다. 왜인지는 안 잡았다 — 잰 대로 둔다.
  # **카탈로그는 하나다 — 엑셀 판과 같은 폴더(~/.magi/catalog), 같은 키.** 같은 머신의 Excel 2021 은 TrustedCatalogs 아래에
  # 키가 둘 이상이면 「설정을 읽는 도중 문제가 발생」이라며 전부 지운다(2026-09-06 실측) — 이 판의 키까지. 두 설치기가
  # 한 폴더에 각자의 매니페스트를 놓고 키 하나를 같이 쓴다.
  $catalog = Join-Path $configDir 'catalog'
  New-Item -ItemType Directory -Force $catalog | Out-Null
  $copy = Join-Path $catalog 'magi-ppt-manifest.xml'
  # 매니페스트가 바뀌었으면 리본 캐시를 지운다 — 안 지우면 PowerPoint 가 옛 이름·옛 단추를 그린다(2026-09-06 실측:
  # 이름을 바꾸고 다시 추가해도 「magi 창」이 남았고, 이 값 둘을 지우고 껐다 켜고 다시 추가하니 새 이름이 섰다).
  $changed = -not (Test-Path $copy) -or ((Get-FileHash $copy).Hash -ne (Get-FileHash $manifest).Hash)
  Copy-Item $manifest $copy -Force
  if ($changed) {
    foreach ($v in ((Get-Item $wef -ErrorAction SilentlyContinue).Property | Where-Object { $_ -like 'PowerPoint_*RibbonCustomizationExpire' -or $_ -eq 'PowerPoint_RibbonCache' })) {
      Remove-ItemProperty -Path $wef -Name $v -ErrorAction SilentlyContinue
    }
    Warn '매니페스트가 바뀌었다 — PowerPoint 를 껐다 켜고 「공유 폴더」에서 다시 추가해야 리본이 새 이름을 그린다'
  }
  # UNC: 준 것 → 이 폴더를 덮는 진짜 공유 → 관리 공유(\\localhost\C$\…, 이 계정이 관리자일 때). 위 -CatalogUnc 주석.
  $unc = $CatalogUnc
  if (-not $unc) {
    $share = Get-SmbShare -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Name -notmatch '\$$' -and $catalog.StartsWith($_.Path.TrimEnd('\') + '\', 'OrdinalIgnoreCase') } | Sort-Object { $_.Path.Length } -Descending | Select-Object -First 1
    if ($share) { $unc = '\\' + $env:COMPUTERNAME + '\' + $share.Name + $catalog.Substring($share.Path.TrimEnd('\').Length) }
  }
  if (-not $unc) {
    $unc = '\\localhost\' + $catalog.Substring(0, 1) + '$' + $catalog.Substring(2)
    Warn "진짜 공유가 없어 관리 공유($unc)를 쓴다. 같은 머신의 Excel 2021 이 이 등록을 켤 때마다 지운다 — 관리자 PowerShell 에서: New-SmbShare -Name magi -Path `"$configDir`" -ReadAccess $env:USERNAME"
  }
  $keys = @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)
  # 우리 것이 아닌 magi 카탈로그 키(옛 자리 ppt-catalog·xl-catalog, 옛 관리 공유 주소)는 지운다 — 키가 둘이면 같은 머신의
  # Excel 2021 이 전부 지우므로 하나만 남겨야 한다. 옛 자리의 폴더는 그대로 둔다(파일은 해가 없다).
  foreach ($old in $keys) {
    $u = (Get-ItemProperty $old.PSPath).Url
    if ($u -and $u -ne $unc -and $u -match '\\\.magi\\(catalog|ppt-catalog|xl-catalog)$') { Remove-Item $old.PSPath -Recurse -Force; Warn "옛 magi 카탈로그 키를 지웠다(키는 하나여야 한다): $u" }
  }
  $keys = @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)
  $mine = $keys | Where-Object { (Get-ItemProperty $_.PSPath).Url -eq $unc } | Select-Object -First 1
  if ($keys.Count -gt ($(if ($mine) { 1 } else { 0 }))) { Warn "다른 신뢰 카탈로그 키가 더 있다($($keys.Count)개). 같은 머신의 Excel 2021 은 키가 둘 이상이면 전부 지운다." }
  if ($mine) { $k = $mine.PSPath; $id = $mine.PSChildName } else { $id = '{' + [guid]::NewGuid().ToString().ToUpperInvariant() + '}'; $k = "$wef\TrustedCatalogs\$id"; New-Item -Path $k -Force | Out-Null }
  Set-ItemProperty $k Id $id; Set-ItemProperty $k Url $unc; Set-ItemProperty $k Flags 1 -Type DWord
  if (-not (Test-Path "$unc\magi-ppt-manifest.xml")) { Warn "$unc 에 닿지 못한다(관리 공유가 막혔나). 폴더를 진짜로 공유하고 Url 을 바꿔라: $k" }
  Done "카탈로그 $unc ($id)"
} else {
  Say '애드인을 개발자 키로 등록한다'
  New-Item -Path "$wef\Developer" -Force | Out-Null
  New-ItemProperty -Path "$wef\Developer" -Name 'magi' -Value $manifest -PropertyType String -Force | Out-Null
  Done "Developer\magi = $manifest"
}

# ── 7. 로그인 때 같이 뜨게 ───────────────────────────────────────────────────
$run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
if ($NoAutostart) {
  Say '자동 시작은 안 건다(-NoAutostart)'
  Remove-ItemProperty $run 'magi-office' -ErrorAction SilentlyContinue
  Remove-ItemProperty $run 'magi-ppt-hand-watch' -ErrorAction SilentlyContinue
} else {
  Say '로그인할 때 헬퍼가 뜨게 한다'
  New-ItemProperty -Path $run -Name 'magi-office' -Value "`"$helperExe`" office -config-dir `"$configDir`"" -PropertyType String -Force | Out-Null
  Done 'Run\magi-office'
  if ($perpetual -and (Test-Path (Join-Path $Dest 'hand\magi-ppt-hand.exe'))) {
    $watch = Join-Path $Dest 'hand-watch.ps1'
    $cmd = "powershell.exe -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$watch`""
    New-ItemProperty -Path $run -Name 'magi-ppt-hand-watch' -Value $cmd -PropertyType String -Force | Out-Null
    Done 'Run\magi-ppt-hand-watch — PowerPoint 가 떠 있으면 손을 붙인다'
    if (-not (Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.CommandLine -like '*hand-watch.ps1*' })) {
      Start-Process powershell.exe -ArgumentList @('-NoProfile', '-WindowStyle', 'Hidden', '-ExecutionPolicy', 'Bypass', '-File', $watch) -WindowStyle Hidden | Out-Null
      Done '손 감시기를 지금 띄웠다'
    }
  }
}

# ── 8. 다음 할 일 ────────────────────────────────────────────────────────────
Write-Host ''
Write-Host '설치 끝. 이제:' -ForegroundColor Cyan
Write-Host '  1. PowerPoint 를 껐다 켠다(떠 있었다면).'
if ($perpetual) {
  Write-Host '  2. 삽입 → 내 추가 기능 → 공유 폴더 에서 Magi(AI Assistant) 를 고르고 「추가」. (한 번만. 매니페스트가 바뀌면 다시)'
  Write-Host '  3. 홈 탭 「AI Assistant」의 「Magi」로 창을 연다. 편집은 COM 손이 한다 — 덱을 연 채로 두면 감시기가 알아서 붙인다.'
} else {
  Write-Host '  2. 홈 탭 → 추가 기능 → 개발자 추가 기능 → magi. (리본에 바로 안 보이면 이 길)'
}
if ($ollama) { Write-Host '  * 처음이면 `ollama signin` 을 한 번 한다(기본 모델이 Ollama 클라우드다).' }
Write-Host "  * 문제가 생기면 docs\INSTALL.ko.md §6, 지우기는 §7. 설치 폴더는 $Dest"
exit 0   # 마지막 네이티브 명령(robocopy 는 1 이 성공)의 코드가 스크립트의 코드로 새지 않게
