<#
.SYNOPSIS
  magi Office 플러그인(파워포인트·엑셀·워드)을 이 계정에 설치한다 — 빌드, 인증서, 애드인 등록, 헬퍼 기동까지 한 번에.

.DESCRIPTION
  헬퍼는 하나다: magi.exe 의 `magi office` 가 포트 3000 에서 /ppt·/xl·/word 세 판을 내준다(clients/office/helper).
  그래서 인증서(office-helper-cert.pem)·자동 시작·신뢰 카탈로그 키가 전부 하나다. 이 파일을 돌리면:
    1. Office 판을 읽어(Microsoft 365 인가 볼륨 판/LTSC 인가) 등록 길을 고른다. 그리고 **사람이 먼저 해야 하는 것**이 있으면
       하라고 말하고 멈춰서 기다린다 — 볼륨 판이면 카탈로그 폴더의 진짜 공유(New-SmbShare, 관리자 한 번), -FromSource 면 Go 와 .NET 9 SDK.
       설치기가 그 결과를 읽어 적는 것들이라 나중에 하면 다시 돌려야 한다. Enter 로 다시 재고 s 로 건너뛴다(-NoWait 는 안 묻는다).
    2. 릴리스에서 magi.exe·추가 기능·어댑터를 받아 설치 폴더에 놓고, 세 애드인의 파일을 그 옆 clients\<앱>\addin 에 복사한다.
    3. **컴패니언 셋의 워크스페이스에만** 설정을 쓴다(<config>\{powerpoint,excel,word}\.magi\config.toml):
       permission = "allow"(사용자 결정 2026-09-05·06)와 [council] enabled = false(2026-09-08).
       전역 config.toml 은 안 건드린다 — 그 파일은 이 사람이 평소 쓰는 magi 의 것이다.
    4. 소켓 자리(~/.magi)를 이 계정의 환경 변수 MAGI_SOCKET_DIR 로 걸고(setx — 평소 magi 도 새 터미널부터 같은 명단을 본다),
       헬퍼를 -config-dir·-socket-dir 로 띄우고, 헬퍼가 만든 인증서를 이 계정의 신뢰 저장소에 넣는다(Windows 가 한 번 묻는다).
    5. 애드인 셋을 등록한다 — M365 는 개발자 키 셋, 볼륨 판은 신뢰 카탈로그 하나(~/.magi/catalog)에 매니페스트 셋.
    6. **Office 를 켤 때 헬퍼가 뜨게 한다** — Office 안에서 뜨는 COM 추가 기능(magi-office-start)이 헬퍼를 띄운다.
       **로그인 때 뜨는 등록은 걸지 않는다**(사용자, 2026-09-07: 「쓰지도 않는데 켜져 있는 건 악성코드 아니냐」).
       Office 를 안 켜면 이 계정에 magi 는 하나도 없고, Office 를 다 끄면 스스로 다 끝난다. -NoAutostart 는 그 등록도 안 한다.

  볼륨 판 PowerPoint(LTSC 2021)는 작업창으로 편집이 안 돼 COM 어댑터(magi-ppt-hand.exe)가 편집한다 — 볼륨 판이면 그 어댑터도
  빌드한다(.NET SDK 필요, 없으면 경고만). 어댑터를 띄우는 것은 헬퍼다: PowerPoint 가 떠 있는데 어댑터가 없으면 헬퍼가 띄우고,
  어댑터가 열린 덱 전부를 맡다가 PowerPoint 가 끝나면 같이 끝난다. 그래서 로그인 때 뜨는 등록은 헬퍼 하나뿐이다(2026-09-07 —
  그 전에는 PowerShell 감시기가 따로 떴다). Excel·Word 2021 은 작업창이 그대로 손이라 어댑터가 없다.

  실측: 2026-09-06 밤 2021(볼륨 판)에서 끝까지 돌았고(그때는 COM 손·소켓 분리 전), 같은 기계에서 세 프로그램의 도구가
  하나씩 전부 돌았다(파워포인트 48·엑셀 76·워드 66 — 각 판 TESTING). COM 손 빌드·감시기·MAGI_SOCKET_DIR 은 2026-09-07 에
  더한 것이라 그 뒤의 실측이 아직 없다.

  다시 돌려도 된다 — 이미 된 것은 건너뛴다.

  ⚠ PowerShell 5.1 은 BOM 없는 UTF-8 을 ANSI 로 읽는다. 이 파일은 BOM 이 있어야 한다.

.PARAMETER Dest
  설치 폴더. 기본 %LOCALAPPDATA%\magi\office

.PARAMETER NoAutostart
  Office 를 켤 때 헬퍼가 뜨게 하는 COM 추가 기능을 **등록하지 않는다**(있던 것은 뺀다). 그러면 Office 창을 열기 전에
  헬퍼를 손으로 띄워야 한다: `magi office`.

.PARAMETER NoWait
  먼저 할 것(진짜 공유, -FromSource 의 툴체인)이 없어도 묻지 않고 간다 — 무인 배포용. 없는 것은 경고로만 남는다.

.PARAMETER SkipDownload
  받기를 건너뛴다 — Dest 에 이미 파일이 있을 때. 옛 이름 -SkipBuild 로도 부를 수 있다.

.PARAMETER FromSource
  릴리스에서 받는 대신 이 저장소에서 짓는다. 그때만 Go 와 .NET 9 SDK 가 필요하다.

.PARAMETER Clean
  애드인을 **지우고 다시 깐다.** 이 판의 등록(신뢰 카탈로그 키·개발자 키)을 빼고 Office 의 애드인 캐시(Wef 폴더)를
  비운 뒤 보통 설치를 이어 간다. Office 프로그램이 떠 있으면 멈춘다.

.PARAMETER Uninstall
  **다 지운다** — 헬퍼·컴패니언·어댑터를 멈추고, COM 추가 기능 등록(Office 를 켤 때 헬퍼를 띄우던 것), 옛 Run 키,
  애드인 등록(개발자 키 셋·신뢰 카탈로그 키), Office 애드인
  캐시, 설치 폴더(Dest), 소켓 자리(~/.magi 의 daemon-*.sock*·catalog), 신뢰 저장소의 인증서(magi office helper), 사용자
  환경 변수 MAGI_SOCKET_DIR 을 뺀다. 남기는 것: %APPDATA%\magi 의 config.toml 과 plugins(평소 magi 의 것이라 이 설치기가
  쓴 적이 없다), 컴패니언 워크스페이스(%APPDATA%\magi\powerpoint·excel·word — 그 안의 .magi\config.toml 이 이 셋의
  permission·council 이고, 대화 기록은 %LOCALAPPDATA%\magi).
  그것까지 지우려면 그 폴더를 손으로.
#>
[CmdletBinding()]
param(
  [string]$Dest = (Join-Path $env:LOCALAPPDATA 'magi\office'),
  [switch]$NoAutostart,
  [Alias('SkipBuild')][switch]$SkipDownload,
  [switch]$FromSource,
  [switch]$Clean,
  [switch]$Uninstall,
  [switch]$NoWait,
  [string]$CatalogUnc = ''
)
# -SkipDownload: 이미 받아 둔 파일을 그대로 쓴다. 옛 이름 -SkipBuild 도 받는다 — 그 이름으로 적어 둔
#   명령줄이 깨지지 않게(이 스위치가 하던 일은 그대로다: 3단계를 통째로 건너뛴다).
# -FromSource: 받는 대신 **이 저장소에서 짓는다**. 그때만 Go 와 .NET 9 SDK 가 필요하다. 저장소를 클론해
#   고치는 사람의 자리이고, 그 사람에게는 방금 고친 것이 배포판보다 중요하다.
# -NoWait: 사람이 먼저 해야 하는 것(진짜 공유·-FromSource 의 툴체인)이 없어도 묻지 않고 그냥 간다(무인 배포).
#   기본은 **멈춰서 기다린다**.
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
# Office 를 켤 때 헬퍼를 띄우는 COM 추가 기능(clients/office/addin-com). 이 셋이 등록의 이름이다.
$startClsid = '{38162D7F-4C03-4B36-9F55-15D83EEA5EF3}'
$startProgId = 'Magi.Office.Start'
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
  Say 'Office 를 켤 때 뜨던 추가 기능 등록을 지웁니다'
  foreach ($ribbon in @('PowerPoint', 'Excel', 'Word')) {
    $k = "HKCU:\Software\Microsoft\Office\$ribbon\Addins\$startProgId"
    if (Test-Path $k) { Remove-Item $k -Recurse -Force -ErrorAction SilentlyContinue; Done "지웠습니다: $ribbon" }
  }
  foreach ($k in @("HKCU:\Software\Classes\CLSID\$startClsid", "HKCU:\Software\Classes\$startProgId")) {
    if (Test-Path $k) { Remove-Item $k -Recurse -Force -ErrorAction SilentlyContinue }
  }
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
# COM 추가 기능은 Office 프로세스 **안에서** 뜨므로 비트 수가 같아야 한다. 대개 x64 다.
$bitness = if ("$($c2r.Platform)" -eq 'x86') { 'x86' } else { 'x64' }
Done "$edition ($ids · $bitness)"
# 릴리스는 x64 자산만 낸다. 예전에는 여기서 x86 으로 지어 줬으므로 이것은 **기능 축소**이고, 그래서
# 조용히 넘기지 않고 사유와 함께 멈춘다 — 조용히 x64 를 깔면 Office 가 추가 기능을 못 읽고, 그 증상은
# 「아무 일도 안 일어난다」 하나로 뭉쳐 나온다.
if ($bitness -eq 'x86' -and -not $FromSource -and -not $SkipDownload) {
  Fail '32비트 Office 입니다. 배포되는 추가 기능은 64비트뿐이고, COM 추가 기능은 Office 프로세스 안에서 뜨므로 비트 수가 같아야 합니다. 이 저장소를 클론해 -FromSource 로 실행하시면 32비트로 지어 드립니다.'
}
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
# 툴체인은 **-FromSource 일 때만** 필요하다. 기본은 릴리스에서 받으므로 Go 도 .NET SDK 도 안 깐다 —
# 이 스크립트가 사람에게 시키던 일의 대부분이 그 둘을 찾고, 없으면 설명하고, 기다리는 것이었다.
$go = $null
if ($FromSource -and -not $SkipDownload) {
  if (WaitUntil 'Go' 'https://go.dev/dl 에서 Go 를 설치해 주세요. -FromSource 없이 실행하면 릴리스에서 받으므로 Go 가 필요 없습니다.' { [bool](Get-Command go -ErrorAction SilentlyContinue) }) {
    $go = Get-Command go; Done "go: $($go.Source)"
  } else { Fail 'Go 가 없어 -FromSource 빌드를 계속할 수 없습니다.' }
}
$dotnet = $null
# .NET SDK 는 이제 판을 안 가린다 — 볼륨 판의 편집 어댑터도, 모든 판의 COM 추가 기능(Office 를 켤 때 헬퍼를 띄우는 것)도
# 이것으로 짓는다. 없으면 추가 기능 없이 가고, 그때는 로그인 등록으로 물러선다(7단계).
# **.NET 9 SDK 는 Go 와 나란한 요구다 — 판을 안 가린다.** 사용자 결정(2026-09-07): 「그냥 닷넷을 필수로 하면 시작
# 프로그램에 등록하는 거 안 해도 된다는 거지?」 — 그렇다. Office 를 켤 때 헬퍼를 띄우는 추가 기능을 이것으로 짓기
# 때문에, 있으면 로그인 등록이 하나도 필요 없고 없으면 물러설 자리가 없다(그 물러섬이 곧 「쓰지도 않는데 켜져 있는 것」
# 이었다). 볼륨 판은 편집 어댑터까지 이것으로 짓는다.
$sdkHow = 'https://dotnet.microsoft.com/download/dotnet/9.0 에서 .NET 9 SDK 를 설치해 주세요(런타임만으로는 부족합니다). -FromSource 없이 실행하면 릴리스에서 받으므로 SDK 가 필요 없습니다.'
if ($FromSource -and -not $SkipDownload) {
  if (WaitUntil '.NET 9 SDK' $sdkHow { [bool](FindDotnet) }) { $dotnet = FindDotnet; Done "dotnet: $($dotnet.FullName) (SDK $($dotnet.Sdk))" }
  if (-not $dotnet -and -not $NoAutostart) {
    Fail '.NET 9 SDK 가 있어야 -FromSource 로 추가 기능을 지을 수 있습니다. SDK 를 설치하거나, -FromSource 를 빼고 릴리스에서 받아 주세요.'
  }
}
if ($perpetual) {
  # 진짜 공유 — 카탈로그의 UNC 를 여기서 읽어 적으므로 나중에 만들면 설치기를 다시 돌려야 한다.
  $catalogDir = Join-Path $socketDir 'catalog'
  if (-not $CatalogUnc) {
    $shareCmd = "관리자 PowerShell 에서 한 번만 실행해 주세요:  New-SmbShare -Name magi -Path `"$socketDir`" -ReadAccess $env:USERNAME"
    if (WaitUntil "폴더 공유($socketDir)" $shareCmd { [bool](RealShareFor $catalogDir) }) { Done "공유 폴더: $((RealShareFor $catalogDir).Name)" }
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

# ── 3. 받기(또는 -FromSource 로 짓기) ────────────────────────────────────────
#
# 기본은 **릴리스에서 받는다**. 예전에는 이 자리가 `go build` 와 `dotnet build` 였고, 그래서 이 스크립트를
# 돌리는 사람은 저장소를 클론하고 툴체인 둘을 깔아야 했다 — 그리고 빌드가 실패하면 기능이 조용히 반쪽이
# 됐다(어댑터가 없으면 PowerPoint 2021 편집만, 추가 기능이 없으면 Office 를 켜도 헬퍼가 안 뜬다).
#
# 판 번호는 **움직이지 않는 주소**에서 읽는다. `/releases/latest` 는 이 저장소의 레인을 안 가려서, 콘솔이
# 더 최근에 나갔으면 office 자산이 404 이고 checksums.txt 는 남의 것을 200 으로 답한다(코어 레인이
# 2026-08-31 에 실측한 그것). badges 브랜치의 한 줄짜리 파일이 그 함정을 통째로 없앤다.
$RepoUrl   = 'https://github.com/sayaya1090/magi'
$BadgesRaw = 'https://raw.githubusercontent.com/sayaya1090/magi/badges'

# 한 줄짜리 판 번호 파일. 못 읽으면 $null — 부르는 쪽이 사유를 말한다.
function LatestTag($file) {
  try {
    $r = Invoke-WebRequest -Uri "$BadgesRaw/$file" -UseBasicParsing -TimeoutSec 20
    $v = "$($r.Content)".Trim()
    if ($v) { return $v }
  } catch { }
  return $null
}

# 자산 하나를 받아 푼다. 받는 자리는 예전 빌드 산출물이 가던 그 자리라, 아래 복사·등록 절이 안 바뀐다.
function GetAsset($tag, $asset, $into) {
  $url = "$RepoUrl/releases/download/$tag/$asset"
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("magi-" + [Guid]::NewGuid().ToString('N') + ".zip")
  try {
    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing -TimeoutSec 300
  } catch {
    Fail "$asset 을 못 받았습니다($url): $($_.Exception.Message)"
  }
  New-Item -ItemType Directory -Force $into | Out-Null
  # 덮어쓴다 — 옛 판의 파일이 섞여 남으면 무엇이 도는지가 파일 날짜로만 갈린다.
  Expand-Archive -LiteralPath $tmp -DestinationPath $into -Force
  Remove-Item $tmp -Force -ErrorAction SilentlyContinue
  Done "$asset → $into"
}

# 추가 기능이 쓰는 .NET 데스크톱 런타임. 없으면 받아서 조용히 깐다.
#
# 추가 기능만 framework-dependent 다(224KB). `--self-contained` 도 comhost.dll 을 내주기는 하지만 SDK 가
# `NETSDK1128: COM 호스팅에서는 자체 포함 배포가 지원되지 않습니다` 를 붙이고, 이 DLL 은 Office 프로세스
# **안에서** 로드된다 — 미지원 경로를 그 자리에 놓지 않기로 했다(2026-09-08). 그 대가가 이 함수다.
# 어댑터는 자체 포함이라 이것과 무관하게 돈다.
function EnsureDesktopRuntime {
  # `--list-runtimes` 에 WindowsDesktop 9 이 있으면 끝. dotnet.exe 가 있다고 런타임이 있는 것이 아니다.
  $have = $false
  foreach ($c in @('dotnet', 'C:\Program Files\dotnet\dotnet.exe')) {
    try {
      $lines = @(& $c --list-runtimes 2>&1 | Where-Object { $_ -is [string] })
      if ($lines | Where-Object { $_ -match '^Microsoft\.WindowsDesktop\.App 9\.' }) { $have = $true; break }
    } catch { }
  }
  if ($have) { Done '.NET 9 데스크톱 런타임: 있음'; return }

  Say '.NET 9 데스크톱 런타임을 받습니다(추가 기능이 씁니다)'
  $url = 'https://aka.ms/dotnet/9.0/windowsdesktop-runtime-win-x64.exe'
  $exe = Join-Path ([IO.Path]::GetTempPath()) 'windowsdesktop-runtime-9-x64.exe'
  try {
    Invoke-WebRequest -Uri $url -OutFile $exe -UseBasicParsing -TimeoutSec 600
  } catch {
    Warn "런타임을 못 받았습니다($url): $($_.Exception.Message). Office 를 켜도 헬퍼가 저절로 뜨지 않을 수 있습니다 — 직접 설치해 주세요."
    return
  }
  # /install /quiet /norestart — MS 의 번들 설치기가 받는 그 셋. 관리자 권한이 없으면 UAC 가 뜬다.
  $p = Start-Process -FilePath $exe -ArgumentList '/install', '/quiet', '/norestart' -Wait -PassThru
  Remove-Item $exe -Force -ErrorAction SilentlyContinue
  # 3010 = 성공했고 재부팅을 권한다. 실패가 아니다.
  if ($p.ExitCode -eq 0 -or $p.ExitCode -eq 3010) { Done '.NET 9 데스크톱 런타임을 설치했습니다' }
  else { Warn "런타임 설치기가 $($p.ExitCode) 로 끝났습니다. Office 를 켜도 헬퍼가 저절로 뜨지 않으면 https://dotnet.microsoft.com/download/dotnet/9.0 에서 데스크톱 런타임을 설치해 주세요." }
}

if ($SkipDownload) {
  Say '받기를 건너뜁니다(-SkipDownload)'
} elseif ($FromSource) {
  Say 'magi 를 빌드합니다(-FromSource)'
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
    # 어댑터가 안 지어져도 설치는 간다(PowerPoint 2021 편집만 안 된다). **$dotnet 을 지우지 않는다** — 아래 추가 기능은
    # 어댑터와 무관하고, 그것까지 못 지으면 헬퍼가 저절로 안 뜬다.
    if ($LASTEXITCODE -ne 0) { Warn "어댑터 빌드에 실패했습니다. 위의 dotnet 오류를 확인해 주세요. 어댑터 없이 계속합니다(PowerPoint 2021 편집만 안 됩니다)." }
    else { Done "magi-ppt-hand.exe → $handOut" }
  }
  if ($dotnet -and -not $NoAutostart) {   # -NoAutostart 면 추가 기능을 안 짓는다 — 등록도 안 할 것이라서다
    Say 'Office 를 켤 때 헬퍼가 뜨게 하는 추가 기능을 빌드합니다'
    $startProj = Join-Path $repo 'clients\office\addin-com\src\magi-office-start.csproj'
    $startOut = Join-Path $Dest 'start'
    & $dotnet.FullName build $startProj -c Release -r "win-$bitness" --self-contained false -o $startOut --nologo -v q
    if ($LASTEXITCODE -ne 0) { Warn '추가 기능 빌드에 실패했습니다. 위의 dotnet 오류를 확인해 주세요. Office 를 켤 때 헬퍼가 저절로 뜨지는 않습니다.' }
    else { Done "magi-office-start.comhost.dll → $startOut" }
  }
} else {
  # 코어(= 헬퍼). `magi office` 가 헬퍼라 이 파일은 코어 바이너리 그 자체다 — 그래서 office 릴리스에
  # 사본을 두지 않고 코어 레인에서 받는다. 두 레인에 같은 파일이 있으면 「어느 쪽이 최신인가」가 태그를
  # 언제 달았느냐로 정해진다.
  Say 'magi 를 받습니다'
  $coreTag = LatestTag 'core-latest.txt'
  if (-not $coreTag) { Fail "코어 판 번호를 못 읽었습니다($BadgesRaw/core-latest.txt). 인터넷 연결을 확인하시거나, 직접 빌드하려면 -FromSource 로 실행해 주세요." }
  Say "  코어 $coreTag"
  GetAsset $coreTag 'magi_windows_amd64.zip' $Dest

  # Office 자산 둘. 비트수는 Office 를 따라간다 — COM 추가 기능은 Office 프로세스 **안에서** 뜬다.
  $officeTag = LatestTag 'office-latest.txt'
  if (-not $officeTag) { Fail "Office 판 번호를 못 읽었습니다($BadgesRaw/office-latest.txt). 인터넷 연결을 확인하시거나, 직접 빌드하려면 -FromSource 로 실행해 주세요." }
  Say "  Office $officeTag"

  if (-not $NoAutostart) {
    GetAsset $officeTag 'magi-office-start_win_x64.zip' (Join-Path $Dest 'start')
    EnsureDesktopRuntime
  }
  if ($perpetual) {
    # PowerPoint 2021 편집 어댑터. 자체 포함이라 .NET 런타임과 무관하게 돈다(76MB).
    GetAsset $officeTag 'magi-ppt-hand_win_x64.zip' (Join-Path $Dest 'hand')
  }
}
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

# ── 4. Office 컴패니언만의 설정 ──────────────────────────────────────────────
#
# **전역 config.toml 을 안 건드린다.** 2026-09-08 까지 이 자리는 `%APPDATA%\magi\config.toml` 에
# `permission = "allow"` 를 썼는데, 그 파일은 이 사람이 평소 쓰는 magi 의 것이다 — 터미널에서 치는
# magi 도, 웹 콘솔의 컴패니언도, 예약 작업도 전부 그 한 줄을 물려받았다. Office 를 깔았다는 이유로
# 그 사람의 모든 magi 가 승인 없이 도는 것은 이 설치기가 결정할 일이 아니다(사용자, 2026-09-08:
# 「퍼미션이 전역에 들어가는건 이상하다」).
#
# 대신 **워크스페이스 층**에 쓴다. 컴패니언 셋은 `<config>\{powerpoint,excel,word}` 에서 돌고,
# magi 는 그 자리의 `.magi\config.toml` 을 전역 위에 얹는다(`loadConfigLayers`). 컴패니언 층
# (`companions\<이름>-<해시>`)이 더 좁지만 경로에 해시가 있어 여기서 계산할 수 없다.
#
# ⚠ 이미 전역에 `permission = "allow"` 가 적힌 머신은 **그대로 둔다.** 지우면 그 사람이 다른 이유로
# 원했을 수도 있는 설정을 이 설치기가 없애는 것이 된다. 새로 쓰지 않을 뿐이다.
Say 'Office 컴패니언 설정을 씁니다'
New-Item -ItemType Directory -Force $configDir | Out-Null
$cfg = Join-Path $configDir 'config.toml'
if (Test-Path $cfg) {
  $has = @(Get-Content $cfg -Encoding UTF8) | Where-Object { $_ -match '^\s*(model|base_url|api_key|profile)\s*=' -or $_ -match '^\s*\[(llm|plugins)' }
  if (-not $has) { Warn "config.toml 에 모델 설정이 없습니다. 기본값으로 동작합니다. ($cfg)" }
}

foreach ($app in $apps) {
  $space = Join-Path $configDir $app.dir          # 컴패니언이 도는 워크스페이스
  $dotMagi = Join-Path $space '.magi'
  New-Item -ItemType Directory -Force $dotMagi | Out-Null
  $wsCfg = Join-Path $dotMagi 'config.toml'

  # 워크스페이스가 **신뢰 목록**에 있어야 permission 을 푸는 방향으로 쓸 수 있다. 신뢰 안 된
  # 워크스페이스의 설정은 가드레일을 조일 수만 있고 풀 수는 없다(mergeProjectConfigSaying) —
  # 그 규칙은 남의 저장소를 클론했을 때를 위한 것이고, 이 셋은 설치기가 방금 만든 자리다.
  $magiExe = Join-Path $Dest 'magi.exe'
  if (Test-Path $magiExe) {
    Push-Location $space
    try { & $magiExe --trust 2>&1 | Out-Null } finally { Pop-Location }
  }

  # 통째로 쓴다. 이 파일은 설치기의 것이고 사람이 고칠 자리가 아니다 — 사람의 설정은 전역에 있다.
  $body = @(
    '# magi Office 설치기가 쓴다. 이 워크스페이스(= 이 컴패니언)에만 걸린다 —',
    '# 사람이 평소 쓰는 magi 의 설정은 <config>\config.toml 이고 이 파일이 그것을 안 덮는다.',
    '',
    '# 승인 창이 흐름을 끊는 품이 더 크다(사용자 결정 2026-09-05·06). 이 셋에만 건다.',
    'permission = "allow"',
    '',
    '# 카운슬은 끈다(사용자 결정 2026-09-08). 문서를 고치는 자리에서는 세 렌즈의 합의보다',
    '# 사람이 화면에서 바로 보는 것이 빠르고, 매 턴 세 번의 모델 호출이 그 값을 못 한다.',
    '# `council` 도구도 함께 사라지고, 그래서 끝냄 선언도 없어진다.',
    '[council]',
    'enabled = false'
  )
  [IO.File]::WriteAllLines($wsCfg, $body, (New-Object Text.UTF8Encoding $false))
  Done "$($app.dir): permission=allow · council=off"
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

# 2026-09-07 에 설정 자리와 소켓 자리를 갈랐다(설정은 %APPDATA%\magi, 소켓만 ~/.magi). **인증서는 설정 자리를
# 따라간다** — 그래서 그 전에 깐 머신은 신뢰 저장소에 든 것이 옛 자리(~/.magi)에 남고, 헬퍼는 새 자리에 만든
# **아무도 안 믿는** 인증서로 TLS 를 선다. 그러면 작업창이 흰 화면이고 데몬도 MCP 문에 못 붙는데(엑셀 판
# TESTING §4b 의 `x509: certificate signed by unknown authority`), 화면에는 아무 사유도 안 뜬다.
# 그래서 **띄우기 전에** 옮긴다: 새 자리에 없거나 있는 것이 안 믿기는데 옛 자리 것이 믿기면, 믿기는 쪽을 쓴다.
$oldPem = Join-Path $socketDir 'office-helper-cert.pem'
$oldKey = Join-Path $socketDir 'office-helper-key.pem'
$newPem = Join-Path $configDir 'office-helper-cert.pem'
$newKey = Join-Path $configDir 'office-helper-key.pem'
function Test-Trusted($path) {
  if (-not (Test-Path $path)) { return $false }
  try {
    $c = New-Object Security.Cryptography.X509Certificates.X509Certificate2 $path
    return [bool](Get-ChildItem Cert:\CurrentUser\Root | Where-Object { $_.Thumbprint -eq $c.Thumbprint })
  } catch { return $false }
}
if ((Test-Path $oldPem) -and (Test-Path $oldKey) -and (Test-Trusted $oldPem) -and -not (Test-Trusted $newPem)) {
  New-Item -ItemType Directory -Force $configDir | Out-Null
  Copy-Item $oldPem $newPem -Force; Copy-Item $oldKey $newKey -Force
  Done '옛 자리의 인증서를 설정 디렉토리로 옮겼습니다(이미 신뢰된 것이라 다시 안 묻습니다)'
}

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

# ── 7. 언제 뜨게 할 것인가 ───────────────────────────────────────────────────
# **Office 를 켤 때 뜬다.** 작업창의 페이지는 헬퍼가 내주므로 헬퍼가 먼저 떠 있어야 하는데, 그것을 로그인 등록으로
# 풀면 Office 를 안 켜는 날에도 magi 가 떠 있다. 사용자가 그것을 물렸다(2026-09-07: 「윈도우 로그인 때 자동 켜지는 거
# 하지 말라고」, 「오피스에서 플러그인 켤 때 COM 이랑 .NET 으로 프로세스 못 띄우냐」). COM 추가 기능은 Office 프로세스
# 안에서 뜨므로 그 자리가 정확히 맞다. 헬퍼는 Office 가 하나도 없으면 스스로 끝나고(helper/idle.go), 컴패니언도 그렇다.
#
# 못 지었으면 **아무것도 안 건다** — 그때는 사람이 `magi office` 를 직접 띄운다. 로그인 등록으로 물러서던 길은 없앴다.
$run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
# 옛 등록은 언제나 뺀다 — 이 설치기는 로그인 때 뜨는 것을 하나도 안 남긴다.
Remove-ItemProperty $run 'magi-ppt-hand-watch' -ErrorAction SilentlyContinue
Remove-ItemProperty $run 'magi-office' -ErrorAction SilentlyContinue
$comhost = Join-Path $Dest 'start\magi-office-start.comhost.dll'
if ($NoAutostart) {
  Say '자동 시작을 등록하지 않습니다(-NoAutostart)'
  foreach ($ribbon in @('PowerPoint', 'Excel', 'Word')) {
    $k = "HKCU:\Software\Microsoft\Office\$ribbon\Addins\$startProgId"
    if (Test-Path $k) { Remove-Item $k -Recurse -Force -ErrorAction SilentlyContinue }
  }
  Warn 'Office 창을 열기 전에 헬퍼를 직접 띄워야 합니다.'
} elseif (Test-Path $comhost) {
  Say 'Office 를 켤 때 헬퍼가 뜨게 등록합니다'
  # 헬퍼의 명령줄은 여기서 적어 둔다 — 설정·소켓 자리를 아는 것은 설치기다(Starter.cs HelperArgs).
  $argsLine = "office -config-dir `"$configDir`" -socket-dir `"$socketDir`""
  [IO.File]::WriteAllText((Join-Path $Dest 'start\helper-args.txt'), $argsLine, (New-Object Text.UTF8Encoding $false))
  # CLSID → comhost.dll, ProgID → CLSID. 전부 이 계정 몫이라 관리자 권한이 필요 없다.
  $inproc = "HKCU:\Software\Classes\CLSID\$startClsid\InprocServer32"
  New-Item -Path $inproc -Force | Out-Null
  New-ItemProperty -Path $inproc -Name '(default)' -Value $comhost -PropertyType String -Force | Out-Null
  New-ItemProperty -Path $inproc -Name 'ThreadingModel' -Value 'Both' -PropertyType String -Force | Out-Null
  New-Item -Path "HKCU:\Software\Classes\CLSID\$startClsid\ProgID" -Force | Out-Null
  New-ItemProperty -Path "HKCU:\Software\Classes\CLSID\$startClsid\ProgID" -Name '(default)' -Value $startProgId -PropertyType String -Force | Out-Null
  New-Item -Path "HKCU:\Software\Classes\$startProgId\CLSID" -Force | Out-Null
  New-ItemProperty -Path "HKCU:\Software\Classes\$startProgId\CLSID" -Name '(default)' -Value $startClsid -PropertyType String -Force | Out-Null
  # LoadBehavior 3 = 시작할 때 불러 온다. Office 가 2 로 내려 두면 다음부터 안 부르므로 매번 3 으로 되돌린다.
  foreach ($ribbon in @('PowerPoint', 'Excel', 'Word')) {
    $k = "HKCU:\Software\Microsoft\Office\$ribbon\Addins\$startProgId"
    New-Item -Path $k -Force | Out-Null
    New-ItemProperty -Path $k -Name 'FriendlyName' -Value 'Magi' -PropertyType String -Force | Out-Null
    New-ItemProperty -Path $k -Name 'Description' -Value 'Magi 헬퍼를 띄웁니다' -PropertyType String -Force | Out-Null
    New-ItemProperty -Path $k -Name 'LoadBehavior' -Value 3 -PropertyType DWord -Force | Out-Null
  }
  Done 'Office 를 켜면 헬퍼가 뜹니다 — 로그인 때 뜨는 등록은 없습니다'
} else {
  # **로그인 등록으로 물러서지 않는다.** 사용자: 「쓰지도 않는데 켜져 있는 건 악성코드 아니냐」. 기본 경로는 추가
  # 기능을 릴리스에서 받으므로, 여기 오는 것은 -SkipDownload 로 깐 판이거나 -FromSource 빌드가 실패한 경우다.
  Warn "추가 기능이 없어 Office 를 켤 때 헬퍼가 자동으로 뜨지 않습니다. Office 를 쓰기 전에 직접 띄우세요: `"$helperExe`" office"
}
if ($perpetual -and -not (Test-Path (Join-Path $Dest 'hand\magi-ppt-hand.exe'))) {
  Warn 'PowerPoint 2021 용 어댑터가 없습니다. 설치기를 다시 실행하시면 릴리스에서 받습니다(-FromSource 면 .NET 9 SDK 가 필요합니다).'
}

# ── 8. 다음 할 일 ────────────────────────────────────────────────────────────
Write-Host ''
Write-Host '설치를 마쳤습니다. 다음 단계:' -ForegroundColor Cyan
Write-Host '  1. PowerPoint·Excel·Word 를 다시 시작합니다. (헬퍼는 Office 를 켤 때 같이 뜹니다)'
if ($perpetual) {
  Write-Host '  2. 각 프로그램에서 삽입 → 내 추가 기능 → 공유 폴더 → Magi(AI Assistant) → 추가. (처음 한 번)'
  Write-Host '  3. 홈 탭의 「Magi」단추로 창을 엽니다. PowerPoint 2021 은 문서를 열어 두면 몇 초 뒤 편집 준비가 됩니다.'
} else {
  Write-Host '  2. 각 프로그램에서 홈 탭 → 추가 기능 → 개발자 추가 기능 → Magi(AI Assistant).'
}
Write-Host "  * 자세한 안내: clients\<앱>\docs\INSTALL.ko.md   설치 폴더: $Dest"
exit 0   # 마지막 네이티브 명령(robocopy 는 1 이 성공)의 코드가 스크립트의 코드로 새지 않게
