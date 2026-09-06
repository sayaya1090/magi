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
while ($true) {
  try {
    $ppt = Get-Process POWERPNT -ErrorAction SilentlyContinue
    $running = Get-Process magi-ppt-hand -ErrorAction SilentlyContinue
    if ($ppt -and -not $running -and (Test-Path $hand)) {
      # 덱이 하나라도 열려 있을 때만 — 없으면 손이 「덱을 먼저 여세요」 하고 바로 끝난다.
      $open = $false
      try { $app = [Runtime.InteropServices.Marshal]::GetActiveObject('PowerPoint.Application'); $open = ($app.Presentations.Count -gt 0) } catch { $open = $false }
      if ($open) {
        Log '덱이 열려 있고 손이 없다 — 붙인다'
        Start-Process -FilePath $hand -ArgumentList @('--helper', $helper) -WorkingDirectory $root -WindowStyle Hidden | Out-Null
        Start-Sleep -Seconds 5
      }
    }
    # PowerPoint 가 내려가면 손도 내려간다(COM 참조가 죽는다). 붙어 있던 손이 남아 있으면 정리한다 — 헬퍼에 유령 덱을 남기지 않게.
    if (-not $ppt -and $running) { Log 'PowerPoint 가 없는데 손이 남아 있다 — 정리한다'; $running | Stop-Process -Force -ErrorAction SilentlyContinue }
  } catch { Log "오류: $($_.Exception.Message)" }
  Start-Sleep -Seconds 4
}
