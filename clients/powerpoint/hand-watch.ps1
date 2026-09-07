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
Log "감시기 시작 — 손: $hand"
# **덱마다 손 하나.** 손 하나가 활성 덱에만 붙으면 다른 창의 부탁이 그 덱에 떨어지고 두 창이 같은 손을 봐 답이 섞인다
# (실물 2026-09-07, 2021 에서 덱 둘). 열린 덱마다 `--presentation <경로>` 로 하나씩 띄우고, 닫힌 덱의 손은 거둔다.
function OpenDecks {
  try { $app = [Runtime.InteropServices.Marshal]::GetActiveObject('PowerPoint.Application') } catch { return @() }
  $out = @(); foreach ($p in $app.Presentations) { try { $out += $p.FullName } catch { } }
  return $out
}
function HandProcesses { Get-CimInstance Win32_Process -Filter "Name='magi-ppt-hand.exe'" -ErrorAction SilentlyContinue }
while ($true) {
  try {
    $ppt = Get-Process POWERPNT -ErrorAction SilentlyContinue
    $hands = @(HandProcesses)
    if ($ppt -and (Test-Path $hand)) {
      $decks = @(OpenDecks)
      foreach ($deck in $decks) {
        $has = $hands | Where-Object { $_.CommandLine -and $_.CommandLine.ToLowerInvariant().Contains($deck.ToLowerInvariant()) }
        if (-not $has) {
          Log "덱이 열려 있고 손이 없다 — 붙인다: $deck"
          Start-Process -FilePath $hand -ArgumentList @('--helper', $helper, '--presentation', "`"$deck`"") -WorkingDirectory $root -WindowStyle Hidden | Out-Null
          Start-Sleep -Seconds 2
        }
      }
      # 닫힌 덱의 손은 거둔다 — 헬퍼에 유령 덱을 남기지 않게. (--presentation 없이 뜬 옛 손은 덱이 하나일 때만 둔다.)
      foreach ($h in $hands) {
        $cmd = if ($h.CommandLine) { $h.CommandLine.ToLowerInvariant() } else { '' }
        $mine = $decks | Where-Object { $cmd.Contains($_.ToLowerInvariant()) }
        $legacy = -not $cmd.Contains('--presentation')
        if (-not $mine -and -not ($legacy -and $decks.Count -eq 1)) { Log "닫힌 덱의 손을 거둔다(pid $($h.ProcessId))"; Stop-Process -Id $h.ProcessId -Force -ErrorAction SilentlyContinue }
      }
    }
    # PowerPoint 가 내려가면 손도 내려간다(COM 참조가 죽는다). 붙어 있던 손이 남아 있으면 정리한다.
    if (-not $ppt -and $hands.Count -gt 0) { Log 'PowerPoint 가 없는데 손이 남아 있다 — 정리한다'; $hands | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue } }
  } catch { Log "오류: $($_.Exception.Message)" }
  Start-Sleep -Seconds 4
}
