# magi-ppt-hand 감시기 — PowerPoint 가 덱을 연 채로 떠 있으면 COM 손을 붙이고, 없으면 기다린다.
#
# 손은 뜰 때 한 번 PowerPoint 에 붙고(InteropOps.AttachToRunning), PowerPoint 가 없거나 덱이 안 열려
# 있으면 그대로 끝난다 — 몰래 PowerPoint 를 띄우지 않는다. 그래서 로그인 때 손을 바로 띄우는 것은
# 소용이 없고, 이 감시기가 대신 서 있다. install.ps1 이 Run 키에 건다. 볼륨 판(LTSC 2021)에서만 필요하다.
#
# ⚠ PowerShell 5.1 은 BOM 없는 UTF-8 을 ANSI 로 읽는다 — 이 파일은 BOM 이 있어야 한다.
$root = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
$hand = Join-Path $root 'hand\magi-ppt-hand.exe'
$helper = 'https://127.0.0.1:3000/ppt'
$log = Join-Path $root 'hand-watch.log'
function Log($s) { try { Add-Content $log ("{0:yyyy-MM-dd HH:mm:ss} {1}" -f (Get-Date), $s) -Encoding UTF8 } catch { } }
Log "감시기를 시작합니다. 어댑터: $hand"
# **덱마다 손 하나.** 손 하나가 활성 덱에만 붙으면 다른 창의 부탁이 그 덱에 떨어지고 두 창이 같은 손을 봐 답이 섞인다
# (실물 2026-09-07, 2021 에서 덱 둘). 열린 덱마다 `--presentation <경로>` 로 하나씩 띄우고, 닫힌 덱의 손은 거둔다.
# COM 으로 못 닿으면 $null(사유는 $script:comWhy), 닿았는데 덱이 없으면 빈 배열 — 둘은 다른 사실이다.
function OpenDecks {
  try { $app = [Runtime.InteropServices.Marshal]::GetActiveObject('PowerPoint.Application') } catch { $script:comWhy = $_.Exception.Message; return $null }
  $out = @(); foreach ($p in $app.Presentations) { try { $out += $p.FullName } catch { } }
  return $out
}
function HandProcesses { Get-CimInstance Win32_Process -Filter "Name='magi-ppt-hand.exe'" -ErrorAction SilentlyContinue }
# **왜 안 붙이는지를 말한다** — 실물 2021(2026-09-07)에서 로그가 「감시기 시작」 한 줄뿐이었고, 세 조건 중 무엇이
# 빠졌는지 아무도 몰랐다. 같은 사유는 한 번만 적는다(4초마다 반복하지 않게).
$lastWhy = ''
function Idle($why) { if ($why -ne $script:lastWhy) { Log $why; $script:lastWhy = $why } }
$elevated = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if ($elevated) { Log '⚠ 관리자 권한으로 실행 중입니다. 일반 권한의 PowerPoint 에 연결할 수 없으니, 설치기를 일반 권한 창에서 다시 실행해 주세요.' }
while ($true) {
  try {
    $ppt = Get-Process POWERPNT -ErrorAction SilentlyContinue
    $hands = @(HandProcesses)
    if (-not $ppt) { Idle 'PowerPoint 가 실행 중이 아닙니다. 기다립니다.' }
    elseif (-not (Test-Path $hand)) { Idle "어댑터 파일이 없습니다: $hand. 설치기를 -SkipBuild 없이 다시 실행해 주세요." }
    else {
      $found = OpenDecks
      if ($null -eq $found) { Idle "PowerPoint 는 실행 중이지만 연결할 수 없습니다($script:comWhy). 권한 수준이 다르면(관리자 창) 이렇게 됩니다." }
      elseif ($found.Count -eq 0) { Idle 'PowerPoint 는 실행 중이지만 열린 문서가 없습니다. 기다립니다.' }
      else { $script:lastWhy = '' }
      $decks = @($found)
      foreach ($deck in $decks) {
        $has = $hands | Where-Object { $_.CommandLine -and $_.CommandLine.ToLowerInvariant().Contains($deck.ToLowerInvariant()) }
        if (-not $has) {
          Log "어댑터를 시작합니다: $deck"
          Start-Process -FilePath $hand -ArgumentList @('--helper', $helper, '--presentation', "`"$deck`"") -WorkingDirectory $root -WindowStyle Hidden | Out-Null
          Start-Sleep -Seconds 2
        }
      }
      # 닫힌 덱의 손, --presentation 없이 뜬 옛 손, 같은 덱에 둘 이상 뜬 손(오래된 쪽)을 거둔다 — 같은 덱에 손이 둘이면
      # 호출을 서로 가로챈다(실물 2021, 2026-09-07). 앞 판은 옛 손을 「덱이 하나면 둔다」고 했는데 그 옆에 새 손을 또
      # 띄워 정확히 그 둘을 만들었다. COM 으로 못 닿은 판에서는 안 거둔다 — 「덱 없음」이 아니라 「모름」이다.
      $seen = @{}
      foreach ($h in $(if ($null -eq $found) { @() } else { $hands | Sort-Object CreationDate -Descending })) {
        $cmd = if ($h.CommandLine) { $h.CommandLine.ToLowerInvariant() } else { '' }
        $mine = @($decks | Where-Object { $cmd.Contains($_.ToLowerInvariant()) })
        $why = ''
        if (-not $cmd.Contains('--presentation')) { $why = '문서를 지정하지 않은 이전 어댑터' }
        elseif ($mine.Count -eq 0) { $why = '닫힌 문서의 어댑터' }
        elseif ($seen[$mine[0]]) { $why = "같은 문서에 중복된 어댑터(오래된 쪽) — $($mine[0])" }
        else { $seen[$mine[0]] = $true }
        if ($why) { Log "$why 를 종료합니다(pid $($h.ProcessId))"; Stop-Process -Id $h.ProcessId -Force -ErrorAction SilentlyContinue }
      }
    }
    # PowerPoint 가 내려가면 손도 내려간다(COM 참조가 죽는다). 붙어 있던 손이 남아 있으면 정리한다.
    if (-not $ppt -and $hands.Count -gt 0) { Log 'PowerPoint 가 종료되어 남은 어댑터를 정리합니다'; $hands | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue } }
  } catch { Log "오류: $($_.Exception.Message)" }
  Start-Sleep -Seconds 4
}
