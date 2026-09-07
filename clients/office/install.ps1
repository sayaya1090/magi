<#
.SYNOPSIS
  magi Office 플러그인(파워포인트·엑셀·워드)을 이 계정에 설치한다 — 빌드, 인증서, 애드인 등록, 헬퍼 기동까지 한 번에.

.DESCRIPTION
  헬퍼는 하나다: magi.exe 의 `magi office` 가 포트 3000 에서 /ppt·/xl·/word 세 판을 내준다(clients/office/helper).
  그래서 인증서(office-helper-cert.pem)·자동 시작·신뢰 카탈로그 키가 전부 하나다. 이 파일을 돌리면:
    1. Office 판을 읽어(Microsoft 365 인가 볼륨 판/LTSC 인가) 등록 길을 고른다. 그리고 **사람이 먼저 해야 하는 것**이 있으면
       하라고 말하고 멈춰서 기다린다 — Go, 볼륨 판이면 카탈로그 폴더의 진짜 공유(New-SmbShare, 관리자 한 번)와 .NET 9 SDK.
       설치기가 그 결과를 읽어 적는 것들이라 나중에 하면 다시 돌려야 한다. Enter 로 다시 재고 s 로 건너뛴다(-NoWait 는 안 묻는다).
    2. magi.exe 를 빌드해 설치 폴더에 놓고, 세 애드인의 파일을 그 옆 clients\<앱>\addin 에 복사한다(헬퍼가 그 자리를 본다).
    3. 데몬 권한 모드를 allow 로 둔다(설정 디렉토리의 config.toml — 평소의 magi 와 같은 파일). 사용자 결정(2026-09-05·06).
       컴패니언은 평소의 magi 와 같은 설정 나무(%APPDATA%\magi)를 보고, 소켓만 ~/.magi(MAGI_SOCKET_DIR)에 둔다.
    4. 소켓 자리(~/.magi)를 이 계정의 환경 변수 MAGI_SOCKET_DIR 로 걸고(setx — 평소 magi 도 새 터미널부터 같은 명단을 본다),
       헬퍼를 -config-dir·-socket-dir 로 띄우고, 헬퍼가 만든 인증서를 이 계정의 신뢰 저장소에 넣는다(Windows 가 한 번 묻는다).
    5. 애드인 셋을 등록한다 — M365 는 개발자 키 셋, 볼륨 판은 신뢰 카탈로그 하나(~/.magi/catalog)에 매니페스트 셋.
    6. 로그인할 때 헬퍼가 같이 뜨게 한다(Run\magi-office). 볼륨 판이면 손 감시기도(Run\magi-ppt-hand-watch). -NoAutostart 로 끈다.

  볼륨 판 PowerPoint(LTSC 2021)는 작업창으로 편집이 안 돼 COM 손(magi-ppt-hand.exe)이 편집한다 — 볼륨 판이면 2단계에서 그 손도
  빌드하고(.NET SDK 필요, 없으면 경고만) hand-watch.ps1 을 헬퍼 옆에 놓는다. 감시기는 PowerPoint 가 덱을 연 채 떠 있으면 손을
  붙이고 PowerPoint 가 내려가면 거둔다. Excel·Word 2021 은 작업창이 그대로 손이라 손이 없다.

  실측: 2026-09-06 밤 2021(볼륨 판)에서 끝까지 돌았고(그때는 COM 손·소켓 분리 전), 같은 기계에서 세 프로그램의 도구가
  하나씩 전부 돌았다(파워포인트 48·엑셀 76·워드 66 — 각 판 TESTING). COM 손 빌드·감시기·MAGI_SOCKET_DIR 은 2026-09-07 에
  더한 것이라 그 뒤의 실측이 아직 없다.

  다시 돌려도 된다 — 이미 된 것은 건너뛴다.

  ⚠ PowerShell 5.1 은 BOM 없는 UTF-8 을 ANSI 로 읽는다. 이 파일은 BOM 이 있어야 한다.

.PARAMETER Dest
  설치 폴더. 기본 %LOCALAPPDATA%\magi\office

.PARAMETER NoAutostart
  로그인 때 자동으로 띄우는 등록(HKCU\...\Run 의 magi-office 와 magi-ppt-hand-watch)을 안 하고, 있던 것은 뺀다.

.PARAMETER NoWait
  먼저 할 것(Go·진짜 공유·.NET SDK)이 없어도 묻지 않고 간다 — 무인 배포용. 없는 것은 경고로만 남는다.

.PARAMETER SkipBuild
  빌드를 건너뛴다 — Dest 에 이미 실행 파일이 있을 때(배포본). COM 손도 안 짓는다: Dest\hand 에 이미 있으면 그것을 쓰고,
  없으면 앞서 걸어 둔 감시기를 도로 띄운다.

.PARAMETER Clean
  애드인을 **지우고 다시 깐다.** 이 판의 등록(신뢰 카탈로그 키·개발자 키)을 빼고 Office 의 애드인 캐시(Wef 폴더)를
  비운 뒤 보통 설치를 이어 간다. Office 프로그램이 떠 있으면 멈춘다.

.PARAMETER Uninstall
  **다 지운다** — 헬퍼·컴패니언·손·감시기를 멈추고, Run 키 둘, 애드인 등록(개발자 키 셋·신뢰 카탈로그 키), Office 애드인
  캐시, 설치 폴더(Dest), 소켓 자리(~/.magi 의 daemon-*.sock*·catalog), 신뢰 저장소의 인증서(magi office helper), 사용자
  환경 변수 MAGI_SOCKET_DIR 을 뺀다. 남기는 것: %APPDATA%\magi 의 config.toml(permission = "allow" 줄까지 — 평소 magi 의
  파일이다)과 plugins, 컴패니언 워크스페이스(%APPDATA%\magi\powerpoint·excel·word — 대화 기록은 %LOCALAPPDATA%\magi).
  그것까지 지우려면 그 폴더를 손으로.
#>
[CmdletBinding()]
param(
  [string]$Dest = (Join-Path $env:LOCALAPPDATA 'magi\office'),
  [switch]$NoAutostart,
  [switch]$SkipBuild,
  [switch]$Clean,
  [switch]$Uninstall,
  [switch]$NoWait,
  [string]$CatalogUnc = ''
)
# -NoWait: 사람이 먼저 해야 하는 것(진짜 공유·.NET SDK·Go)이 없어도 묻지 않고 그냥 간다(무인 배포). 기본은 **멈춰서 기다린다**.
# -CatalogUnc: 카탈로그 폴더(~/.magi/catalog)가 보이는 **진짜 공유**의 UNC. 비우면 이 계정의 공유 목록에서
# 그 폴더를 덮는 공유를 찾고, 없으면 관리 공유(\\<컴퓨터>\C$\…)를 쓴다. Excel 2021 은 관리 공유 형태의 카탈로그를
# 켤 때마다 지운다(2026-09-06 실측 — localhost 도 컴퓨터 이름도) — 진짜 공유가 필요하다:
#   New-SmbShare -Name magi -Path "$env:USERPROFILE\.magi" -ReadAccess $env:USERNAME   (관리자 PowerShell)

$ErrorActionPreference = 'Stop'
$repo = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$apps = @(
  @{ key = 'ppt';  dir = 'powerpoint'; exe = 'POWERPNT.EXE'; proc = 'POWERPNT'; ribbon = 'PowerPoint' },
  @{ key = 'xl';   dir = 'excel';      exe = 'EXCEL.EXE';    proc = 'EXCEL';    ribbon = 'Excel' },
  @{ key = 'word'; dir = 'word';       exe = 'WINWORD.EXE';  proc = 'WINWORD';  ribbon = 'Word' }
)
# **설정은 평소의 magi 것, 소켓만 짧은 자리.** 설정 디렉토리는 magi 의 기본값(%APPDATA%\magi, MAGI_CONFIG_DIR 존중)이다 —
# 사람이 평소 쓰는 magi 와 같은 config.toml·plugins 를 본다(키는 파일에 없어도 된다: 플러그인이 실행 뒤에 넣는다).
# 소켓·명단 파일은 ~/.magi(MAGI_SOCKET_DIR)에 둔다 — 유닉스 주소는 100바이트라 %APPDATA%\magi 아래는 긴 사용자
# 이름에서 넘친다(2026-09-02 실측). 2026-09-07 까지는 설정 디렉토리 자체를 ~/.magi 로 못 박아서, 평소 데몬은 되는데
# 컴패니언 셋만 「API 키가 없다」였다. 카탈로그도 ~/.magi 아래에 둔다(공유로 내주는 폴더).
$configDir = if ($env:MAGI_CONFIG_DIR) { $env:MAGI_CONFIG_DIR } else { Join-Path $env:APPDATA 'magi' }
$socketDir = if ($env:MAGI_SOCKET_DIR) { $env:MAGI_SOCKET_DIR } else { Join-Path $env:USERPROFILE '.magi' }
$port = 3000
$helperUrl = "https://127.0.0.1:$port"
$helperName = 'magi'   # 헬퍼도 magi.exe 다 — `magi office`
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Say($s) { Write-Host "▸ $s" }
function Done($s) { Write-Host "  ✓ $s" -ForegroundColor Green }
function Warn($s) { Write-Host "  ⚠ $s" -ForegroundColor Yellow }
function Fail($s) { Write-Host "  ✗ $s" -ForegroundColor Red; exit 1 }
# **사람이 먼저 해야 하는 것은 하라고 말하고 기다린다.** 설치기가 그 결과를 읽어 적는 것(공유의 UNC, SDK 로 짓는 손)은
# 나중에 하면 설치기를 한 번 더 돌려야 한다 — 그러느니 여기서 멈춘다(사용자, 2026-09-07). Enter 로 다시 재고, s 로
# 건너뛴다(그때의 결과는 부르는 쪽이 정한다). 사람이 없는 창(-NoWait·비대화형)에서는 경고만 남기고 간다.
function WaitUntil($what, $howTo, [scriptblock]$check) {
  if (& $check) { return $true }
  if ($NoWait -or -not [Environment]::UserInteractive) { Warn "$what 이(가) 없습니다. $howTo"; return $false }
  while ($true) {
    Write-Host ''
    Write-Host "  ■ 먼저 필요합니다: $what" -ForegroundColor Cyan
    Write-Host "    $howTo"
    $ans = Read-Host '    준비되면 Enter, 건너뛰려면 s'
    if ($ans -match '^[sS]') { Warn "${what}: 건너뜁니다"; return $false }
    if (& $check) { return $true }
    Write-Host '    아직 확인되지 않습니다.' -ForegroundColor Yellow
  }
}
# **dotnet.exe 가 있다고 SDK 가 있는 것이 아니다.** 런타임만 깐 머신에도 호스트는 있고, x86 호스트가 PATH 앞에 서면 x64
# SDK 를 못 본다 — 그때 `dotnet build` 는 「It was not possible to find any installed .NET SDKs」로 죽는다(#181, 2026-09-07).
# 그래서 후보마다 `--list-sdks` 로 **SDK 가 실제로 보이는 것**을 고른다. 없으면 $null.
function FindDotnet {
  $candidates = @()
  $onPath = Get-Command dotnet -ErrorAction SilentlyContinue
  if ($onPath) { $candidates += $onPath.Source }
  $candidates += 'C:\Program Files\dotnet\dotnet.exe'
  if ($env:DOTNET_ROOT) { $candidates += (Join-Path $env:DOTNET_ROOT 'dotnet.exe') }
  $candidates += (Join-Path $env:LOCALAPPDATA 'Microsoft\dotnet\dotnet.exe')
  foreach ($c in ($candidates | Select-Object -Unique)) {
    if (-not (Test-Path $c)) { continue }
    # $ErrorActionPreference = 'Stop' 아래에서 네이티브 명령의 stderr 는 던진다(PS 5.1) — 삼키고 SDK 줄("9.0.xxx [경로]")만 남긴다.
    $sdks = @()
    try { $sdks = @(& $c --list-sdks 2>&1 | Where-Object { ($_ -is [string]) -and ($_ -match '^\d') }) } catch { $sdks = @() }
    if ($sdks.Count -gt 0) { return [pscustomobject]@{ FullName = $c; Sdk = ($sdks | Select-Object -Last 1) } }
  }
  return $null
}
# 그 폴더를 덮는 **진짜 공유**(관리 공유 C$ 가 아닌 것). Excel 2021 은 관리 공유 카탈로그를 켤 때마다 지운다.
function RealShareFor($folder) {
  Get-SmbShare -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Name -notmatch '\$$' -and $folder.StartsWith($_.Path.TrimEnd('\') + '\', 'OrdinalIgnoreCase') } | Sort-Object { $_.Path.Length } -Descending | Select-Object -First 1
}

# ── 지우기(-Uninstall) — 깐 것의 역순. 설정 파일과 플러그인, 대화 기록은 남긴다(위 .PARAMETER Uninstall). ─────
if ($Uninstall) {
  Say '실행 중인 magi 프로그램을 멈춥니다'
  Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.ProcessId -ne $PID -and $_.CommandLine -like '*hand-watch.ps1*' } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; Done "감시기를 멈췄습니다 (pid $($_.ProcessId))" }
  $destFull = if (Test-Path $Dest) { (Resolve-Path $Dest).Path } else { $Dest }
  foreach ($name in @('magi-ppt-hand', 'magi')) {
    Get-Process $name -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith($destFull, 'OrdinalIgnoreCase') } |
      ForEach-Object { Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue; Done "$name 을(를) 멈췄습니다 (pid $($_.Id))" }
  }
  Start-Sleep -Milliseconds 500
  Say '자동 시작 등록을 지웁니다'
  $run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
  foreach ($k in @('magi-office', 'magi-ppt-hand-watch', 'magi-ppt', 'magi-xl', 'magi-word')) { Remove-ItemProperty $run $k -ErrorAction SilentlyContinue }
  Done '지웠습니다'
  Say 'Office 추가 기능 등록을 지웁니다'
  $wef = 'HKCU:\Software\Microsoft\Office\16.0\WEF'
  foreach ($k in @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)) {
    $u = (Get-ItemProperty $k.PSPath).Url
    if ($u -and $u -match '\\\.magi\\(catalog|ppt-catalog|xl-catalog)$') { Remove-Item $k.PSPath -Recurse -Force; Done "지웠습니다: $u" }
  }
  foreach ($old in @('magi-ppt', 'magi-xl', 'magi-word', 'magi')) {
    if (Get-ItemProperty "$wef\Developer" -Name $old -ErrorAction SilentlyContinue) { Remove-ItemProperty "$wef\Developer" -Name $old; Done "지웠습니다: 개발자 등록 $old" }
  }
  $cache = Join-Path $env:LOCALAPPDATA 'Microsoft\Office\16.0\Wef'
  if (Test-Path $cache) { Get-ChildItem $cache -Force | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue; Done "Office 캐시를 비웠습니다" }
  Say '설치 폴더와 인증서를 지웁니다'
  if (Test-Path $Dest) { Remove-Item $Dest -Recurse -Force -ErrorAction SilentlyContinue; Done "지웠습니다: $Dest" }
  if (Test-Path $socketDir) {
    Get-ChildItem $socketDir -Filter 'daemon-*.sock*' -Force -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    $cat = Join-Path $socketDir 'catalog'
    if (Test-Path $cat) { Remove-Item $cat -Recurse -Force -ErrorAction SilentlyContinue }
    Done "지웠습니다: $socketDir 의 연결 파일"
  }
  foreach ($p in @((Join-Path $configDir 'office-helper-cert.pem'), (Join-Path $configDir 'office-helper-key.pem'))) { if (Test-Path $p) { Remove-Item $p -Force -ErrorAction SilentlyContinue } }
  Get-ChildItem Cert:\CurrentUser\Root -ErrorAction SilentlyContinue | Where-Object { $_.Subject -like '*magi office helper*' -or $_.Subject -like '*magi-ppt helper*' -or $_.Subject -like '*magi-xl helper*' -or $_.Subject -like '*magi-word helper*' } |
    ForEach-Object { Remove-Item $_.PSPath -ErrorAction SilentlyContinue; Done "인증서를 지웠습니다" }
  [Environment]::SetEnvironmentVariable('MAGI_SOCKET_DIR', $null, 'User'); Done 'MAGI_SOCKET_DIR 환경 변수를 지웠습니다'
  Write-Host ''
  Write-Host '삭제를 마쳤습니다. 설정 파일(%APPDATA%\magi)과 대화 기록(%LOCALAPPDATA%\magi)은 남겨 두었습니다.' -ForegroundColor Cyan
  Write-Host '  Office 프로그램을 다시 시작하면 Magi 단추가 사라집니다.'
  exit 0
}

# ── 0. 어느 Office 인가 ───────────────────────────────────────────────────────
Say '설치된 Office 를 확인합니다'
$c2r = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Office\ClickToRun\Configuration' -ErrorAction SilentlyContinue
if (-not $c2r) { Fail 'Office 를 찾지 못했습니다. Office 2019 이상 또는 Microsoft 365 가 필요합니다.' }
$ids = "$($c2r.ProductReleaseIds)"
$perpetual = ($ids -match 'Volume|2019|2021|2024')      # 볼륨 판/LTSC — 개발자 키를 무시한다
$edition = if ($perpetual) { "볼륨 판 $($c2r.VersionToReport)" } else { "Microsoft 365 $($c2r.VersionToReport)" }
Done "$edition ($ids)"
foreach ($app in $apps) {
  if (-not (Test-Path (Join-Path $env:ProgramFiles "Microsoft Office\root\Office16\$($app.exe)")) -and -not (Test-Path (Join-Path ${env:ProgramFiles(x86)} "Microsoft Office\root\Office16\$($app.exe)"))) {
    Warn "$($app.ribbon) 이(가) 설치되어 있지 않은 것 같습니다. 그래도 계속합니다."
  }
}
if ($perpetual) { Done 'PowerPoint 2021 은 편집용 어댑터(magi-ppt-hand)가 함께 필요합니다. 아래에서 만듭니다.' }

# ── 1. 먼저 할 것 — 사람이 해야 하는 것은 여기서 멈춰서 기다린다 ─────────────────
Say '필요한 것을 확인합니다'
# 관리자 창에서 돌리고 있나 — 여기서 띄우는 감시기의 권한 수준이 그것을 물려받는다(아래 감시기 자리).
$elevated = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if ($elevated) { Warn '관리자 권한으로 실행 중입니다. 이 설치는 관리자 권한이 필요 없으며, 일반 권한으로 실행하는 것을 권합니다.' }
$go = $null
if (-not $SkipBuild) {
  if (WaitUntil 'Go' 'https://go.dev/dl 에서 Go 를 설치해 주세요. 이미 빌드된 파일이 있으면 -SkipBuild 로 건너뛸 수 있습니다.' { [bool](Get-Command go -ErrorAction SilentlyContinue) }) {
    $go = Get-Command go; Done "go: $($go.Source)"
  } else { Fail 'Go 가 없어 설치를 계속할 수 없습니다.' }
}
$dotnet = $null
if ($perpetual) {
  # 진짜 공유 — 카탈로그의 UNC 를 여기서 읽어 적으므로 나중에 만들면 설치기를 다시 돌려야 한다.
  $catalogDir = Join-Path $socketDir 'catalog'
  if (-not $CatalogUnc) {
    $shareCmd = "관리자 PowerShell 에서 한 번만 실행해 주세요:  New-SmbShare -Name magi -Path `"$socketDir`" -ReadAccess $env:USERNAME"
    if (WaitUntil "폴더 공유($socketDir)" $shareCmd { [bool](RealShareFor $catalogDir) }) { Done "공유 폴더: $((RealShareFor $catalogDir).Name)" }
  }
  # .NET SDK — COM 손을 여기서 지으므로 나중에 깔면 설치기를 다시 돌려야 한다.
  if (-not $SkipBuild) {
    $sdkHow = 'https://dotnet.microsoft.com/download/dotnet/9.0 에서 .NET 9 SDK(x64)를 설치해 주세요. 런타임만으로는 부족합니다. 건너뛰면 PowerPoint 2021 에서 편집이 되지 않습니다.'
    if (WaitUntil '.NET 9 SDK' $sdkHow { [bool](FindDotnet) }) { $dotnet = FindDotnet; Done "dotnet: $($dotnet.FullName) (SDK $($dotnet.Sdk))" }
  }
}

# ── 2. 전에 깔린 것을 멈춘다(설치 폴더의 실행 파일만) ─────────────────────────
Say '이전에 설치된 프로그램을 멈춥니다'
New-Item -ItemType Directory -Force $Dest | Out-Null
$destFull = (Resolve-Path $Dest).Path
# 손 감시기부터 멈춘다 — 안 멈추면 아래서 손을 멈추자마자 감시기가 다시 띄워 COM 손 빌드가 파일 잠금으로 죽는다
# (파워포인트 판 설치기 2026-09-06 실측). 끝에서 다시 띄운다.
Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.ProcessId -ne $PID -and $_.CommandLine -like '*-File*' -and $_.CommandLine -like '*hand-watch.ps1*' } |
  ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; Done "감시기를 멈췄습니다 (pid $($_.ProcessId))" }
# 데몬(magi.exe)도 설치 폴더의 것이면 멈춘다 — 헬퍼가 거기서 띄운 것이고, 안 멈추면 새 magi.exe 를 못 쓴다.
foreach ($name in @('magi', 'magi-ppt-hand')) {
  Get-Process $name -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith($destFull, 'OrdinalIgnoreCase') } |
    ForEach-Object { Stop-Process -Id $_.Id -Force; $_.WaitForExit(5000) | Out-Null; Done "$name 을(를) 멈췄습니다 (pid $($_.Id))" }
}
Start-Sleep -Milliseconds 500
$other = Get-Process magi -ErrorAction SilentlyContinue | Where-Object { -not $_.HasExited }
if ($other) { Warn "다른 magi 가 실행 중입니다(pid $($other.Id -join ',')). 그것이 포트 $port 를 쓰고 있으면 이 설치본이 동작하지 않습니다. 필요하면 종료해 주세요: Stop-Process -Name magi" }
foreach ($old in @('magi-ppt', 'magi-xl', 'magi-word')) {
  $p = Get-Process $old -ErrorAction SilentlyContinue
  if ($p) { $p | Stop-Process -Force; Done "이전 헬퍼 $old 를 멈췄습니다" }
  Remove-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' $old -ErrorAction SilentlyContinue
}

# ── 3. 빌드·복사 ─────────────────────────────────────────────────────────────
if (-not $SkipBuild) {
  Say 'magi 를 빌드합니다'
  Push-Location $repo
  try {
    & go build -o (Join-Path $Dest 'magi.exe') ./cmd/magi
    if ($LASTEXITCODE -ne 0) { Fail 'magi 빌드에 실패했습니다.' }
  } finally { Pop-Location }
  Done "magi.exe → $Dest"
  if ($perpetual -and $dotnet) {
    Say 'PowerPoint 2021 용 어댑터(magi-ppt-hand)를 빌드합니다'
    $dn = $dotnet.FullName
    $handProj = Join-Path $repo 'clients\powerpoint\hand-com\src\magi-ppt-hand.csproj'
    $handOut = Join-Path $Dest 'hand'
    & $dn build $handProj -c Release -o $handOut --nologo -v q
    # 손이 안 지어져도 설치는 간다 — Excel·Word 와 파워포인트 작업창은 손과 무관하다(#181). 옛 손이 있으면 그것을 쓴다.
    if ($LASTEXITCODE -ne 0) { Warn "어댑터 빌드에 실패했습니다. 위의 dotnet 오류를 확인해 주세요. 어댑터 없이 계속합니다(PowerPoint 2021 편집만 안 됩니다)."; $dotnet = $null }
    else { Done "magi-ppt-hand.exe → $handOut" }
  }
} else { Say '빌드를 건너뜁니다(-SkipBuild)' }
if (-not (Test-Path (Join-Path $Dest 'magi.exe'))) { Fail "$Dest\magi.exe 가 없습니다." }

Say '추가 기능 파일을 복사합니다'
$manifests = @{}
foreach ($app in $apps) {
  $addinDest = Join-Path $Dest "clients\$($app.dir)\addin"
  & robocopy (Join-Path $repo "clients\$($app.dir)\addin") $addinDest /MIR /NFL /NDL /NJH /NJS /NP | Out-Null   # robocopy 는 0~7 이 성공
  if ($LASTEXITCODE -ge 8) { Fail "추가 기능 파일 복사에 실패했습니다(robocopy $LASTEXITCODE): $($app.dir)" }
  $manifests[$app.key] = Join-Path $addinDest 'manifest.xml'
  Done "$($app.dir)\addin → $addinDest"
}
# 손 감시기는 헬퍼 옆에 산다 — $Dest\hand\magi-ppt-hand.exe 를 자기 옆에서 찾는다(hand-watch.ps1 의 $root).
Copy-Item (Join-Path $repo 'clients\powerpoint\hand-watch.ps1') (Join-Path $Dest 'hand-watch.ps1') -Force

# ── 4. 데몬 권한 모드 = allow ────────────────────────────────────────────────
Say '설정을 확인합니다'
New-Item -ItemType Directory -Force $configDir | Out-Null
$cfg = Join-Path $configDir 'config.toml'
# 백엔드는 이 파일이나 플러그인이 정한다 — 컴패니언은 평소의 magi 와 같은 나무를 보므로 여기서 가져올 것이 없다.
if (Test-Path $cfg) {
  $has = @(Get-Content $cfg -Encoding UTF8) | Where-Object { $_ -match '^\s*(model|base_url|api_key|profile)\s*=' -or $_ -match '^\s*\[(llm|plugins)' }
  if (-not $has) { Warn "config.toml 에 모델 설정이 없습니다. 기본값으로 동작합니다. ($cfg)" }
}
$lines = @(); if (Test-Path $cfg) { $lines = @(Get-Content $cfg -Encoding UTF8) }
$live = $lines | Where-Object { $_ -match '^\s*permission\s*=' }
if ($live) {
  if ($live -notmatch '"allow"') {
    $lines = $lines | ForEach-Object { if ($_ -match '^\s*permission\s*=') { 'permission = "allow"   # magi 플러그인 설치기가 바꿨다 — 사용자 결정' } else { $_ } }
    [IO.File]::WriteAllLines($cfg, $lines, (New-Object Text.UTF8Encoding $false)); Done 'config.toml: permission 을 allow 로 바꿨습니다'
  } else { Done 'config.toml: 이미 설정되어 있습니다' }
} else {
  $lines += ''; $lines += '# magi Office 플러그인 설치기가 더했다(사용자 결정: 승인 창이 흐름을 끊는 품이 더 크다)'; $lines += 'permission = "allow"'
  [IO.File]::WriteAllLines($cfg, $lines, (New-Object Text.UTF8Encoding $false)); Done 'config.toml: permission = "allow" 를 추가했습니다'
}

# ── 5. 헬퍼를 띄우고 인증서를 넣는다 ─────────────────────────────────────────
Say '헬퍼를 시작합니다'
$helperExe = Join-Path $Dest 'magi.exe'
New-Item -ItemType Directory -Force $socketDir | Out-Null
# 이 계정의 모든 magi 가 같은 소켓 자리를 보게 한다(평소 데몬과 컴패니언이 한 명단이어야 hand_off 가 된다). setx 는 새
# 프로세스부터 보이므로 헬퍼에는 플래그로도 준다.
& setx MAGI_SOCKET_DIR $socketDir | Out-Null
Done "MAGI_SOCKET_DIR = $socketDir"
$helperArgs = @('office', '-config-dir', $configDir, '-socket-dir', $socketDir)
$listening = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
if (-not $listening) {
  Start-Process -FilePath $helperExe -ArgumentList $helperArgs -WorkingDirectory $Dest -WindowStyle Hidden | Out-Null
}
$pem = Join-Path $configDir 'office-helper-cert.pem'
$deadline = (Get-Date).AddSeconds(20)
while (-not (Test-Path $pem) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
if (-not (Test-Path $pem)) { Fail "인증서 파일이 만들어지지 않았습니다($pem). 헬퍼 로그를 확인해 주세요." }
# 살아 있는지는 curl.exe 로 묻고 없으면 TCP — PowerShell 5.1 의 Invoke-WebRequest 는 이 서버와 악수를 못 한다.
$curl = Get-Command curl.exe -ErrorAction SilentlyContinue
$deadline = (Get-Date).AddSeconds(20); $up = $false
while (-not $up -and (Get-Date) -lt $deadline) {
  if ($curl) {
    $code = & $curl.Source -sk -o NUL -w '%{http_code}' --max-time 3 "$helperUrl/word/taskpane.html" 2>$null
    if ("$code" -eq '200') { $up = $true }
  } else {
    $tcp = New-Object Net.Sockets.TcpClient
    try { $tcp.Connect('127.0.0.1', $port); $up = $true } catch { } finally { $tcp.Close() }
  }
  if (-not $up) { Start-Sleep -Milliseconds 700 }
}
if ($up) { Done "헬퍼가 실행 중입니다: $helperUrl" }
else { Fail "헬퍼가 응답하지 않습니다($helperUrl). 포트 $port 를 쓰는 다른 프로그램이 있으면 종료하고 다시 실행해 주세요: Stop-Process -Name magi" }

Say '인증서를 등록합니다'
$cert = New-Object Security.Cryptography.X509Certificates.X509Certificate2 $pem
$have = Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Thumbprint -eq $cert.Thumbprint }
if ($have) { Done "이미 등록되어 있습니다" }
else {
  Write-Host '  Windows 가 인증서 설치를 확인하는 창을 띄우면 「예」를 눌러 주세요. (이 컴퓨터의 127.0.0.1 에만 쓰는 인증서입니다)' -ForegroundColor Cyan
  Import-Certificate -FilePath $pem -CertStoreLocation Cert:\CurrentUser\Root | Out-Null
  $have = Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Thumbprint -eq $cert.Thumbprint }
  if ($have) { Done '등록했습니다' } else { Fail '인증서가 등록되지 않았습니다. 취소하셨다면 다시 실행해 주세요.' }
}

# ── 6. 애드인 등록 ───────────────────────────────────────────────────────────
$wef = 'HKCU:\Software\Microsoft\Office\16.0\WEF'
if ($Clean) {
  Say '이전 등록과 Office 캐시를 지웁니다(-Clean)'
  foreach ($app in $apps) { if (Get-Process $app.proc -ErrorAction SilentlyContinue) { Fail "$($app.ribbon) 이(가) 실행 중입니다. 종료한 뒤 다시 실행해 주세요." } }
  foreach ($k in @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)) {
    $u = (Get-ItemProperty $k.PSPath).Url
    if ($u -and $u -match '\\\.magi\\(catalog|ppt-catalog|xl-catalog)$') { Remove-Item $k.PSPath -Recurse -Force; Done "지웠습니다: $u" }
  }
  foreach ($old in @('magi-ppt', 'magi-xl', 'magi-word')) {
    if (Get-ItemProperty "$wef\Developer" -Name $old -ErrorAction SilentlyContinue) { Remove-ItemProperty "$wef\Developer" -Name $old; Done "지웠습니다: 개발자 등록 $old" }
  }
  foreach ($v in ((Get-Item $wef -ErrorAction SilentlyContinue).Property | Where-Object { $_ -like '*RibbonCustomizationExpire' -or $_ -like '*_RibbonCache' })) {
    Remove-ItemProperty -Path $wef -Name $v -ErrorAction SilentlyContinue
  }
  $cache = Join-Path $env:LOCALAPPDATA 'Microsoft\Office\16.0\Wef'
  if (Test-Path $cache) { Get-ChildItem $cache -Force | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue; Done "Office 캐시를 비웠습니다" }
}
if ($perpetual) {
  Say 'Office 에 추가 기능을 등록합니다'
  # **카탈로그는 하나다.** Excel 2021(16.0.14334)은 TrustedCatalogs 아래에 키가 둘 이상이면 「설정을 읽는 도중 문제가
  # 발생」이라며 **전부 지운다**(2026-09-06 실측). 그래서 ~/.magi/catalog 하나에 매니페스트 셋을 놓고 키 하나를 쓴다.
  $catalog = Join-Path $socketDir 'catalog'
  New-Item -ItemType Directory -Force $catalog | Out-Null
  $changed = $false
  foreach ($app in $apps) {
    $copy = Join-Path $catalog "magi-$($app.key)-manifest.xml"
    if (-not (Test-Path $copy) -or ((Get-FileHash $copy).Hash -ne (Get-FileHash $manifests[$app.key]).Hash)) { $changed = $true }
    Copy-Item $manifests[$app.key] $copy -Force
  }
  if ($changed) {
    foreach ($v in ((Get-Item $wef -ErrorAction SilentlyContinue).Property | Where-Object { $_ -like '*RibbonCustomizationExpire' -or $_ -like '*_RibbonCache' })) {
      Remove-ItemProperty -Path $wef -Name $v -ErrorAction SilentlyContinue
    }
    Warn '추가 기능이 바뀌었습니다. 프로그램을 다시 시작한 뒤 「공유 폴더」에서 다시 추가해 주세요.'
  }
  # UNC: 준 것 → 이 폴더를 덮는 진짜 공유 → 관리 공유. Excel 2021 은 관리 공유 카탈로그를 켤 때마다 지운다(위 -CatalogUnc).
  $unc = $CatalogUnc
  if (-not $unc) {
    $share = Get-SmbShare -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Name -notmatch '\$$' -and $catalog.StartsWith($_.Path.TrimEnd('\') + '\', 'OrdinalIgnoreCase') } | Sort-Object { $_.Path.Length } -Descending | Select-Object -First 1
    if ($share) { $unc = '\\' + $env:COMPUTERNAME + '\' + $share.Name + $catalog.Substring($share.Path.TrimEnd('\').Length) }
  }
  if (-not $unc) {
    $unc = '\\localhost\' + $catalog.Substring(0, 1) + '$' + $catalog.Substring(2)
    Warn "공유 폴더가 없어 임시 경로($unc)를 씁니다. Excel 2021 에서는 이 방식이 유지되지 않으니, 관리자 PowerShell 에서 다음을 실행한 뒤 다시 설치해 주세요: New-SmbShare -Name magi -Path `"$socketDir`" -ReadAccess $env:USERNAME"
  }
  $keys = @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)
  # 우리 것이 아닌 magi 카탈로그 키(옛 자리 ppt-catalog·xl-catalog, 옛 관리 공유 주소)는 지운다 — 키가 둘이면 Excel 이
  # 전부 지우므로 하나만 남겨야 한다.
  foreach ($old in $keys) {
    $u = (Get-ItemProperty $old.PSPath).Url
    if ($u -and $u -ne $unc -and $u -match '\\\.magi\\(catalog|ppt-catalog|xl-catalog)$') { Remove-Item $old.PSPath -Recurse -Force; Warn "이전 등록을 지웠습니다: $u" }
  }
  $keys = @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)
  $mine = $keys | Where-Object { (Get-ItemProperty $_.PSPath).Url -eq $unc } | Select-Object -First 1
  if ($keys.Count -gt ($(if ($mine) { 1 } else { 0 }))) { Warn "다른 신뢰 카탈로그 등록이 더 있습니다($($keys.Count)개). Excel 2021 은 등록이 둘 이상이면 모두 지우니, 나머지를 정리하거나 -Clean 으로 다시 설치해 주세요." }
  if ($mine) { $k = $mine.PSPath; $id = $mine.PSChildName } else { $id = '{' + [guid]::NewGuid().ToString().ToUpperInvariant() + '}'; $k = "$wef\TrustedCatalogs\$id"; New-Item -Path $k -Force | Out-Null }
  Set-ItemProperty $k Id $id; Set-ItemProperty $k Url $unc; Set-ItemProperty $k Flags 1 -Type DWord
  if (-not (Test-Path "$unc\magi-word-manifest.xml")) { Warn "$unc 에 접근할 수 없습니다. 폴더를 공유한 뒤 다시 실행해 주세요." }
  Done "등록했습니다: $unc"
} else {
  Say 'Office 에 추가 기능을 등록합니다(개발자 모드)'
  New-Item -Path "$wef\Developer" -Force | Out-Null
  foreach ($app in $apps) {
    New-ItemProperty -Path "$wef\Developer" -Name "magi-$($app.key)" -Value $manifests[$app.key] -PropertyType String -Force | Out-Null
    Done "등록했습니다: $($app.ribbon)"
  }
}

# ── 7. 로그인 때 같이 뜨게 ───────────────────────────────────────────────────
$run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
if ($NoAutostart) {
  Say '자동 시작을 등록하지 않습니다(-NoAutostart)'
  Remove-ItemProperty $run 'magi-office' -ErrorAction SilentlyContinue
  Remove-ItemProperty $run 'magi-ppt-hand-watch' -ErrorAction SilentlyContinue
} else {
  Say '로그인할 때 자동으로 시작되게 합니다'
  New-ItemProperty -Path $run -Name 'magi-office' -Value "`"$helperExe`" office -config-dir `"$configDir`" -socket-dir `"$socketDir`"" -PropertyType String -Force | Out-Null
  Done '헬퍼 자동 시작'
  if ($perpetual -and (Test-Path (Join-Path $Dest 'hand\magi-ppt-hand.exe'))) {
    # PowerPoint 2021 의 손은 뜰 때 한 번만 PowerPoint 에 붙으므로 감시기가 필요하다 — PowerPoint 가 덱을 연 채 떠
    # 있으면 손을 붙이고, PowerPoint 가 내려가면 손을 거둔다(hand-watch.ps1). 로그인 때 같이 뜨고, 지금도 띄운다.
    $watch = Join-Path $Dest 'hand-watch.ps1'
    $cmd = "powershell.exe -NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$watch`""
    New-ItemProperty -Path $run -Name 'magi-ppt-hand-watch' -Value $cmd -PropertyType String -Force | Out-Null
    Done '어댑터 자동 시작'
    if (-not (Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.CommandLine -like '*hand-watch.ps1*' })) {
      # **보통 권한으로 띄운다.** 이 설치기를 관리자 창에서 돌리면 여기서 뜬 감시기도 관리자이고, 관리자 프로세스는
      # 보통 권한 PowerPoint 의 COM(ROT)을 못 본다 — 감시기 로그가 「감시기 시작」 한 줄로 끝나던 자리(실물 2021,
      # 2026-09-07). runas /trustlevel 은 관리자 토큰을 벗긴 기본 토큰으로 띄운다; 관리자가 아니면 그냥 띄운다.
      if ($elevated) { Start-Process runas.exe -ArgumentList @('/trustlevel:0x20000', $cmd) -WindowStyle Hidden | Out-Null }
      else { Start-Process powershell.exe -ArgumentList @('-NoProfile', '-WindowStyle', 'Hidden', '-ExecutionPolicy', 'Bypass', '-File', $watch) -WindowStyle Hidden | Out-Null }
      Done "어댑터 감시기를 시작했습니다"
    }
  } elseif ($perpetual) {
    # 여기서 손을 못 지었어도(.NET SDK 없음, -SkipBuild) 앞서 걸어 둔 감시기(이전 실행이나 옛 파워포인트 판 설치기의 것)가
    # 있을 수 있다 — 2단계에서 그 감시기를 멈췄으니 **도로 띄운다.** 안 그러면 다음 로그인까지 손이 안 붙는다(2021,
    # 2026-09-07: 「피피티 핸드가 자동으로 안 켜져」 — 통합 설치기가 옛 감시기를 죽이고 제 것은 안 띄운 자리).
    $prev = (Get-ItemProperty $run -Name 'magi-ppt-hand-watch' -ErrorAction SilentlyContinue).'magi-ppt-hand-watch'
    if ($prev) {
      if (-not (Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" | Where-Object { $_.CommandLine -like '*hand-watch.ps1*' })) {
        Start-Process cmd.exe -ArgumentList @('/c', $prev) -WindowStyle Hidden | Out-Null
        Done "이전 감시기를 다시 시작했습니다"
      }
    } else {
      Warn 'PowerPoint 2021 용 어댑터가 없습니다. .NET 9 SDK 를 설치한 뒤 다시 실행해 주세요.'
    }
  }
}

# ── 8. 다음 할 일 ────────────────────────────────────────────────────────────
Write-Host ''
Write-Host '설치를 마쳤습니다. 다음 단계:' -ForegroundColor Cyan
Write-Host '  1. PowerPoint·Excel·Word 를 다시 시작합니다.'
if ($perpetual) {
  Write-Host '  2. 각 프로그램에서 삽입 → 내 추가 기능 → 공유 폴더 → Magi(AI Assistant) → 추가. (처음 한 번)'
  Write-Host '  3. 홈 탭의 「Magi」단추로 창을 엽니다. PowerPoint 2021 은 문서를 열어 두면 몇 초 뒤 편집 준비가 됩니다.'
} else {
  Write-Host '  2. 각 프로그램에서 홈 탭 → 추가 기능 → 개발자 추가 기능 → Magi(AI Assistant).'
}
Write-Host "  * 자세한 안내: clients\<앱>\docs\INSTALL.ko.md   설치 폴더: $Dest"
exit 0   # 마지막 네이티브 명령(robocopy 는 1 이 성공)의 코드가 스크립트의 코드로 새지 않게
