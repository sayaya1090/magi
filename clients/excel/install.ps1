<#
.SYNOPSIS
  magi Excel 플러그인을 이 계정에 설치한다 — 빌드, 인증서, 애드인 등록, 헬퍼 기동까지 한 번에.

.DESCRIPTION
  파워포인트 판 install.ps1 의 엑셀 말이다. 다른 것은 셋뿐이다: 헬퍼가 magi-xl(포트 3001, 인증서 xl-helper-cert.pem),
  카탈로그가 ~/.magi/xl-catalog, 그리고 **COM 손이 없다** — Excel 2021 은 ExcelApi 1.14 라 작업창이 그대로 손이다
  (docs/INSTALL.ko.md §1). 저장소 루트에서(또는 어디서든) 이 파일을 돌리면:
    1. Office 판을 읽어(Microsoft 365 인가 볼륨 판/LTSC 인가) 등록 길을 고른다.
    2. magi.exe · magi-xl.exe 를 빌드해 설치 폴더에 놓고, 애드인 파일을 그 옆에 복사한다.
    3. 데몬 권한 모드를 allow 로 둔다(~/.magi/config.toml). 사용자 결정(2026-09-05·06).
    4. 헬퍼를 띄우고, 헬퍼가 만든 인증서를 이 계정의 신뢰 저장소에 넣는다(Windows 가 한 번 묻는다).
    5. 애드인을 등록한다 — M365 는 개발자 키, 볼륨 판은 신뢰 카탈로그(공유 폴더).
    6. 로그인할 때 헬퍼가 같이 뜨게 한다. -NoAutostart 로 끌 수 있다.

  파워포인트 판과 같이 깔아도 된다 — 포트·인증서·카탈로그·Run 키 이름이 다 다르다. magi.exe 는 둘이 같은 파일을
  각자의 폴더에 갖는다(헬퍼가 자기 옆의 것을 먼저 본다).

  다시 돌려도 된다 — 이미 된 것은 건너뛴다.

  ⚠ PowerShell 5.1 은 BOM 없는 UTF-8 을 ANSI 로 읽는다. 이 파일은 BOM 이 있어야 한다.

.PARAMETER Dest
  설치 폴더. 기본 %LOCALAPPDATA%\magi\xl

.PARAMETER NoAutostart
  로그인 때 자동으로 띄우는 등록(HKCU\...\Run)을 안 한다.

.PARAMETER SkipBuild
  빌드를 건너뛴다 — Dest 에 이미 실행 파일이 있을 때(배포본).

.PARAMETER Clean
  애드인을 **지우고 다시 깐다.** 이 판의 등록(신뢰 카탈로그 키·개발자 키)을 빼고 Office 의 애드인 캐시(Wef 폴더)를
  비운 뒤 보통 설치를 이어 간다. 캐시는 파워포인트 판과 같은 폴더라 그쪽도 다시 추가해야 한다. Excel 이 떠 있으면 멈춘다.
#>
[CmdletBinding()]
param(
  [string]$Dest = (Join-Path $env:LOCALAPPDATA 'magi\xl'),
  [switch]$NoAutostart,
  [switch]$SkipBuild,
  [switch]$Clean,
  [string]$CatalogUnc = ''
)
# -CatalogUnc: 카탈로그 폴더(~/.magi/xl-catalog)가 보이는 **진짜 공유**의 UNC. 비우면 이 계정의 공유 목록에서
# 그 폴더를 덮는 공유를 찾고, 없으면 관리 공유(\\<컴퓨터>\C$\…)를 쓴다. Excel 2021 은 관리 공유 형태의 카탈로그를
# 켤 때마다 지운다(2026-09-06 실측 — localhost 도 컴퓨터 이름도) — 진짜 공유가 필요하다:
#   New-SmbShare -Name magi -Path "$env:USERPROFILE\.magi" -ReadAccess $env:USERNAME   (관리자 PowerShell)

$ErrorActionPreference = 'Stop'
$repo = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$addinSrc = Join-Path $PSScriptRoot 'addin'
# 설정 디렉토리는 ~/.magi 로 못 박는다 — 헬퍼의 기본값(%APPDATA%\magi) 아래에는 AF_UNIX 소켓이 안 서고,
# 거기 새 인증서를 만들어 버린다(파워포인트 판 설치기가 잰 것).
$configDir = if ($env:MAGI_CONFIG_DIR) { $env:MAGI_CONFIG_DIR } else { Join-Path $env:USERPROFILE '.magi' }
$port = 3001
$helperUrl = "https://127.0.0.1:$port"
$helperName = 'magi-xl'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Say($s) { Write-Host "▸ $s" }
function Done($s) { Write-Host "  ✓ $s" -ForegroundColor Green }
function Warn($s) { Write-Host "  ⚠ $s" -ForegroundColor Yellow }
function Fail($s) { Write-Host "  ✗ $s" -ForegroundColor Red; exit 1 }

# ── 0. 어느 Office 인가 ───────────────────────────────────────────────────────
Say 'Office 판을 읽는다'
$c2r = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Office\ClickToRun\Configuration' -ErrorAction SilentlyContinue
if (-not $c2r) { Fail 'Office(클릭 투 런)가 안 보인다. Excel 2019 이상 또는 Microsoft 365 가 필요하다.' }
$ids = "$($c2r.ProductReleaseIds)"
$perpetual = ($ids -match 'Volume|2019|2021|2024')      # 볼륨 판/LTSC — 개발자 키를 무시한다
$edition = if ($perpetual) { "볼륨 판 $($c2r.VersionToReport)" } else { "Microsoft 365 $($c2r.VersionToReport)" }
Done "$edition ($ids)"
if (-not (Test-Path (Join-Path $env:ProgramFiles 'Microsoft Office\root\Office16\EXCEL.EXE')) -and -not (Test-Path (Join-Path ${env:ProgramFiles(x86)} 'Microsoft Office\root\Office16\EXCEL.EXE'))) {
  Warn 'EXCEL.EXE 가 보이지 않는다 — Excel 이 안 깔린 Office 인가? 깔고 다시 돌려라. (등록은 그대로 진행한다)'
}
if ($perpetual) { Done 'Excel 2021/2024 는 ExcelApi 1.14 — 작업창이 그대로 손이다. COM 손은 없다.' }

# ── 1. 도구 ─────────────────────────────────────────────────────────────────
Say '필요한 도구를 본다'
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $SkipBuild -and -not $go) { Fail 'Go 가 없다(go.dev/dl). 빌드된 실행 파일이 이미 있으면 -SkipBuild.' }
if ($go) { Done "go: $($go.Source)" }
$ollama = Get-Command ollama -ErrorAction SilentlyContinue
if ($ollama) { Done "ollama: $($ollama.Source)" } else { Warn 'ollama 가 없다. 기본 모델은 Ollama 클라우드(gpt-oss:120b-cloud)라 ollama 를 깔고 `ollama signin` 을 한 번 해야 한다. 다른 백엔드를 쓰면 ~/.magi/config.toml 의 model/base_url.' }

# ── 2. 전에 깔린 것을 멈춘다(설치 폴더의 실행 파일만) ─────────────────────────
Say '설치 폴더의 옛 프로세스를 멈춘다'
New-Item -ItemType Directory -Force $Dest | Out-Null
$destFull = (Resolve-Path $Dest).Path
# 데몬(magi.exe)도 설치 폴더의 것이면 멈춘다 — 헬퍼가 거기서 띄운 것이고, 안 멈추면 새 magi.exe 를 못 쓴다.
foreach ($name in @($helperName, 'magi')) {
  Get-Process $name -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith($destFull, 'OrdinalIgnoreCase') } |
    ForEach-Object { Stop-Process -Id $_.Id -Force; $_.WaitForExit(5000) | Out-Null; Done "$name (pid $($_.Id)) 멈춤" }
}
Start-Sleep -Milliseconds 500
$other = Get-Process $helperName -ErrorAction SilentlyContinue | Where-Object { -not $_.HasExited }
if ($other) { Warn "다른 곳의 $helperName(pid $($other.Id -join ','))가 떠 있다. 그것을 끄지 않으면 설치본 헬퍼는 조용히 물러난다: Stop-Process -Name $helperName" }

# ── 3. 빌드·복사 ─────────────────────────────────────────────────────────────
if (-not $SkipBuild) {
  Say 'magi 와 헬퍼를 빌드한다'
  Push-Location $repo
  try {
    & go build -o (Join-Path $Dest 'magi.exe') ./cmd/magi
    if ($LASTEXITCODE -ne 0) { Fail 'magi 빌드 실패' }
    & go build -o (Join-Path $Dest "$helperName.exe") ./clients/excel/helper
    if ($LASTEXITCODE -ne 0) { Fail '헬퍼 빌드 실패' }
  } finally { Pop-Location }
  Done "magi.exe · $helperName.exe → $Dest"
} else { Say '빌드는 건너뛴다(-SkipBuild)' }
foreach ($exe in @('magi.exe', "$helperName.exe")) { if (-not (Test-Path (Join-Path $Dest $exe))) { Fail "$Dest\$exe 가 없다" } }

Say '애드인 파일을 헬퍼 옆에 놓는다'
$addinDest = Join-Path $Dest 'addin'
& robocopy $addinSrc $addinDest /MIR /NFL /NDL /NJH /NJS /NP | Out-Null   # robocopy 는 0~7 이 성공
if ($LASTEXITCODE -ge 8) { Fail "애드인 복사 실패(robocopy $LASTEXITCODE)" }
Done "addin → $addinDest"

# ── 4. 데몬 권한 모드 = allow ────────────────────────────────────────────────
Say '데몬 권한 모드를 allow 로 둔다'
New-Item -ItemType Directory -Force $configDir | Out-Null
$cfg = Join-Path $configDir 'config.toml'
$lines = @(); if (Test-Path $cfg) { $lines = @(Get-Content $cfg -Encoding UTF8) }
$live = $lines | Where-Object { $_ -match '^\s*permission\s*=' }
if ($live) {
  if ($live -notmatch '"allow"') {
    $lines = $lines | ForEach-Object { if ($_ -match '^\s*permission\s*=') { 'permission = "allow"   # magi 플러그인 설치기가 바꿨다 — 사용자 결정' } else { $_ } }
    [IO.File]::WriteAllLines($cfg, $lines, (New-Object Text.UTF8Encoding $false)); Done 'config.toml: permission 을 allow 로 바꿨다'
  } else { Done 'config.toml: 이미 allow' }
} else {
  $lines += ''; $lines += '# magi Office 플러그인 설치기가 더했다(사용자 결정: 승인 창이 흐름을 끊는 품이 더 크다)'; $lines += 'permission = "allow"'
  [IO.File]::WriteAllLines($cfg, $lines, (New-Object Text.UTF8Encoding $false)); Done 'config.toml: permission = "allow" 를 더했다'
}

# ── 5. 헬퍼를 띄우고 인증서를 넣는다 ─────────────────────────────────────────
Say '헬퍼를 띄운다'
$helperExe = Join-Path $Dest "$helperName.exe"
$helperArgs = @('-config-dir', $configDir)
if (-not (Get-Process $helperName -ErrorAction SilentlyContinue)) {
  Start-Process -FilePath $helperExe -ArgumentList $helperArgs -WorkingDirectory $Dest -WindowStyle Hidden | Out-Null
}
$pem = Join-Path $configDir 'xl-helper-cert.pem'
$deadline = (Get-Date).AddSeconds(20)
while (-not (Test-Path $pem) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
if (-not (Test-Path $pem)) { Fail "헬퍼가 인증서를 안 만들었다($pem). 헬퍼 로그를 봐라." }
# 살아 있는지는 curl.exe 로 묻고 없으면 TCP — PowerShell 5.1 의 Invoke-WebRequest 는 이 서버와 악수를 못 한다.
$curl = Get-Command curl.exe -ErrorAction SilentlyContinue
$deadline = (Get-Date).AddSeconds(20); $up = $false
while (-not $up -and (Get-Date) -lt $deadline) {
  if ($curl) {
    $code = & $curl.Source -sk -o NUL -w '%{http_code}' --max-time 3 "$helperUrl/taskpane.html" 2>$null
    if ("$code" -eq '200') { $up = $true }
  } else {
    $tcp = New-Object Net.Sockets.TcpClient
    try { $tcp.Connect('127.0.0.1', $port); $up = $true } catch { } finally { $tcp.Close() }
  }
  if (-not $up) { Start-Sleep -Milliseconds 700 }
}
if ($up) { Done "헬퍼가 $helperUrl 에 떠 있다" }
else { Fail "헬퍼가 $helperUrl 에 안 답한다. 다른 것이 $port 번을 쥐고 있으면 그것을 끄고 다시: Stop-Process -Name $helperName" }

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
if ($Clean) {
  Say '이 판의 애드인 등록과 Office 캐시를 지운다(-Clean)'
  if (Get-Process EXCEL -ErrorAction SilentlyContinue) { Fail 'Excel 이 떠 있다 — 끄고 다시 돌려라(캐시를 쥐고 있다)' }
  foreach ($k in @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)) {
    $u = (Get-ItemProperty $k.PSPath).Url
    if ($u -and $u -match '\\xl-catalog$') { Remove-Item $k.PSPath -Recurse -Force; Done "카탈로그 키 $($k.PSChildName) 삭제 ($u)" }
  }
  if (Get-ItemProperty "$wef\Developer" -Name 'magi-xl' -ErrorAction SilentlyContinue) { Remove-ItemProperty "$wef\Developer" -Name 'magi-xl'; Done 'Developer\magi-xl 삭제' }
  foreach ($v in ((Get-Item $wef -ErrorAction SilentlyContinue).Property | Where-Object { $_ -like 'Excel_*RibbonCustomizationExpire' -or $_ -eq 'Excel_RibbonCache' })) {
    Remove-ItemProperty -Path $wef -Name $v -ErrorAction SilentlyContinue
  }
  $cache = Join-Path $env:LOCALAPPDATA 'Microsoft\Office\16.0\Wef'
  if (Test-Path $cache) { Get-ChildItem $cache -Force | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue; Done "캐시 비움 $cache (파워포인트 판도 다시 추가해야 한다)" }
}
$manifest = Join-Path $addinDest 'manifest.xml'
if ($perpetual) {
  Say '애드인을 신뢰 카탈로그로 등록한다(볼륨 판은 개발자 키를 무시한다)'
  $catalog = Join-Path $configDir 'xl-catalog'
  New-Item -ItemType Directory -Force $catalog | Out-Null
  $copy = Join-Path $catalog 'magi-xl-manifest.xml'
  $changed = -not (Test-Path $copy) -or ((Get-FileHash $copy).Hash -ne (Get-FileHash $manifest).Hash)
  Copy-Item $manifest $copy -Force
  if ($changed) {
    foreach ($v in ((Get-Item $wef -ErrorAction SilentlyContinue).Property | Where-Object { $_ -like 'Excel_*RibbonCustomizationExpire' -or $_ -eq 'Excel_RibbonCache' })) {
      Remove-ItemProperty -Path $wef -Name $v -ErrorAction SilentlyContinue
    }
    Warn '매니페스트가 바뀌었다 — Excel 을 껐다 켜고 「공유 폴더」에서 다시 추가해야 리본이 새 것을 그린다'
  }
  # UNC: 준 것 → 이 폴더를 덮는 진짜 공유 → 관리 공유. Excel 2021 은 관리 공유 카탈로그를 켤 때마다 지운다(위 -CatalogUnc).
  $unc = $CatalogUnc
  if (-not $unc) {
    $share = Get-SmbShare -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Name -notmatch '\$$' -and $catalog.StartsWith($_.Path.TrimEnd('\') + '\', 'OrdinalIgnoreCase') } | Sort-Object { $_.Path.Length } -Descending | Select-Object -First 1
    if ($share) { $unc = '\\' + $env:COMPUTERNAME + '\' + $share.Name + $catalog.Substring($share.Path.TrimEnd('\').Length) }
  }
  if (-not $unc) {
    $unc = '\\localhost\' + $catalog.Substring(0, 1) + '$' + $catalog.Substring(2)
    Warn "진짜 공유가 없어 관리 공유($unc)를 쓴다. Excel 2021 은 이 형태를 켤 때마다 지운다 — 관리자 PowerShell 에서: New-SmbShare -Name magi -Path `"$configDir`" -ReadAccess $env:USERNAME 하고 다시 돌려라"
  }
  $keys = @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)
  foreach ($old in $keys) {
    $u = (Get-ItemProperty $old.PSPath).Url
    if ($u -and $u -ne $unc -and $u -match '\\xl-catalog$' -and -not (Test-Path $u)) { Remove-Item $old.PSPath -Recurse -Force; Warn "닿지 않는 옛 카탈로그 키를 지웠다: $u" }
  }
  $keys = @(Get-ChildItem "$wef\TrustedCatalogs" -ErrorAction SilentlyContinue)
  $mine = $keys | Where-Object { (Get-ItemProperty $_.PSPath).Url -eq $unc } | Select-Object -First 1
  if ($mine) { $k = $mine.PSPath; $id = $mine.PSChildName } else { $id = '{' + [guid]::NewGuid().ToString() + '}'; $k = "$wef\TrustedCatalogs\$id"; New-Item -Path $k -Force | Out-Null }
  Set-ItemProperty $k Id $id; Set-ItemProperty $k Url $unc; Set-ItemProperty $k Flags 1 -Type DWord
  if (-not (Test-Path "$unc\magi-xl-manifest.xml")) { Warn "$unc 에 닿지 못한다(관리 공유가 막혔나). 폴더를 진짜로 공유하고 Url 을 바꿔라: $k" }
  Done "카탈로그 $unc ($id)"
} else {
  Say '애드인을 개발자 키로 등록한다'
  New-Item -Path "$wef\Developer" -Force | Out-Null
  New-ItemProperty -Path "$wef\Developer" -Name 'magi-xl' -Value $manifest -PropertyType String -Force | Out-Null
  Done "Developer\magi-xl = $manifest"
}

# ── 7. 로그인 때 같이 뜨게 ───────────────────────────────────────────────────
$run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
if ($NoAutostart) {
  Say '자동 시작은 안 건다(-NoAutostart)'
  Remove-ItemProperty $run $helperName -ErrorAction SilentlyContinue
} else {
  Say '로그인할 때 헬퍼가 뜨게 한다'
  New-ItemProperty -Path $run -Name $helperName -Value "`"$helperExe`" -config-dir `"$configDir`"" -PropertyType String -Force | Out-Null
  Done "Run\$helperName"
}

# ── 8. 다음 할 일 ────────────────────────────────────────────────────────────
Write-Host ''
Write-Host '설치 끝. 이제:' -ForegroundColor Cyan
Write-Host '  1. Excel 을 껐다 켠다(떠 있었다면).'
if ($perpetual) {
  Write-Host '  2. 삽입 → 내 추가 기능 → 공유 폴더 에서 Magi(AI Assistant) 를 고르고 「추가」. (한 번만. 매니페스트가 바뀌면 다시)'
  Write-Host '  3. 홈 탭 「AI Assistant」의 「Magi」로 창을 연다. 편집은 창이 직접 한다(ExcelApi 1.14).'
} else {
  Write-Host '  2. 홈 탭 → 추가 기능 → 개발자 추가 기능 → Magi(AI Assistant). (리본에 바로 안 보이면 이 길)'
}
if ($ollama) { Write-Host '  * 처음이면 `ollama signin` 을 한 번 한다(기본 모델이 Ollama 클라우드다).' }
Write-Host "  * 자세한 것은 docs\INSTALL.ko.md. 설치 폴더는 $Dest"
exit 0   # 마지막 네이티브 명령(robocopy 는 1 이 성공)의 코드가 스크립트의 코드로 새지 않게
