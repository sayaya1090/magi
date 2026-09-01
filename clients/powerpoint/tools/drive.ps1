param([string]$Do = "open", [string]$Text = "", [string]$Shot = "shot")
$ErrorActionPreference = 'Continue'
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Drawing, System.Windows.Forms
Add-Type -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
[DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, int dwExtraInfo);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
[DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
'@ -Name UD -Namespace WD

$A = [System.Windows.Automation.AutomationElement]
$T = [System.Windows.Automation.TreeScope]
function Click($x, $y) {
  [WD.UD]::SetCursorPos($x, $y); Start-Sleep -Milliseconds 400
  [WD.UD]::mouse_event(0x0002, 0, 0, 0, 0); [WD.UD]::mouse_event(0x0004, 0, 0, 0, 0)
}
function ClickEl($e) { $r = $e.Current.BoundingRectangle; Click ([int]($r.X + $r.Width / 2)) ([int]($r.Y + $r.Height / 2)) }
function Win { [System.Windows.Automation.AutomationElement]::RootElement.FindFirst($T::Children,
    (New-Object System.Windows.Automation.PropertyCondition($A::ClassNameProperty, "PPTFrameClass"))) }
function ById($w, $id) { $w.FindFirst($T::Descendants, (New-Object System.Windows.Automation.PropertyCondition($A::AutomationIdProperty, $id))) }
function ByName($w, $n) {
  $found = @()
  foreach ($e in $w.FindAll($T::Descendants, [System.Windows.Automation.Condition]::TrueCondition)) {
    if ($e.Current.Name -eq $n) { $found += $e }
  }
  $found
}
function Front {
  $p = Get-Process POWERPNT -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p) { [WD.UD]::ShowWindow($p.MainWindowHandle, 3) | Out-Null; [WD.UD]::SetForegroundWindow($p.MainWindowHandle) | Out-Null }
  Start-Sleep -Seconds 2
}
function Shot($name) {
  $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
  $bmp = New-Object System.Drawing.Bitmap($b.Width, $b.Height); $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen(0, 0, 0, 0, (New-Object System.Drawing.Size($b.Width, $b.Height)))
  $cw = 520; $ch = 910; $oy = 80
  $crop = New-Object System.Drawing.Bitmap($cw, $ch); $g2 = [System.Drawing.Graphics]::FromImage($crop)
  $g2.DrawImage($bmp, (New-Object System.Drawing.Rectangle 0, 0, $cw, $ch),
    (New-Object System.Drawing.Rectangle ($b.Width - $cw), $oy, $cw, $ch), [System.Drawing.GraphicsUnit]::Pixel)
  $crop.Save("C:\Users\velve\Workspace\ppt-test\$name.png", [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose(); $g2.Dispose(); $crop.Dispose()
  "shot $name"
}

# 작업창(WebView) 안의 좌표는 창 기준으로 잡는다 — 창을 옮기거나 크기를 바꿔도 안 깨진다.
function PaneBox {
  $w = Win
  $pane = ById $w "MsoWebViewControl"
  if (-not $pane) {
    foreach ($e in $w.FindAll($T::Descendants, [System.Windows.Automation.Condition]::TrueCondition)) {
      if ($e.Current.ClassName -like "*WebView*" -or $e.Current.ClassName -like "*Chrome_*") { $pane = $e; break }
    }
  }
  if ($pane) { $pane.Current.BoundingRectangle } else { $null }
}

switch ($Do) {
  "open" {
    Front
    $w = Win
    $tab = ByName $w "홈"
    if ($tab.Count -gt 0) { $tab[0].GetCurrentPattern([System.Windows.Automation.SelectionItemPattern]::Pattern).Select(); Start-Sleep -Seconds 3 }
    $fly = ById $w "OfficeExtensionsShowAddinFlyout"
    if (-not $fly) { "no flyout button"; exit 1 }
    ClickEl $fly; Start-Sleep -Seconds 6
    $w = Win
    $hits = ByName $w "magi"
    if ($hits.Count -eq 0) { Shot "no-magi"; "magi not in flyout"; exit 1 }
    ClickEl $hits[0]; Start-Sleep -Seconds 18
    Shot $Shot
  }
  "close" {
    Front
    $w = Win
    $x = ByName $w "작업창 닫기"
    if ($x.Count -eq 0) { $x = ByName $w "Close" }
    if ($x.Count -eq 0) { "no close button"; exit 1 }
    ClickEl $x[0]; Start-Sleep -Seconds 3
    "closed"
  }
  "say" {
    Front
    $r = PaneBox
    if (-not $r) { "no pane"; exit 1 }
    Set-Clipboard -Value $Text
    # 컴포저는 판 아래에 고정이다: 텍스트 상자와 보내기 단추의 자리를 판 사각형에서 뽑는다.
    $tx = [int]($r.X + $r.Width / 2)
    $ty = [int]($r.Y + $r.Height - 145)
    Click $tx $ty; Start-Sleep -Milliseconds 600
    [System.Windows.Forms.SendKeys]::SendWait("^v"); Start-Sleep -Seconds 2
    $sx = [int]($r.X + $r.Width - 55)
    $sy = [int]($r.Y + $r.Height - 84)
    Click $sx $sy; Start-Sleep -Seconds 4
    Shot $Shot
  }
  "full" {
    Front
    $w = Win
    $r = $w.Current.BoundingRectangle
    $bmp = New-Object System.Drawing.Bitmap([int]$r.Width, [int]$r.Height)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen([int]$r.X, [int]$r.Y, 0, 0, (New-Object System.Drawing.Size([int]$r.Width, [int]$r.Height)))
    $bmp.Save((Join-Path "C:\Users\velve\Workspace\ppt-test" ($Shot + ".png")), [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    "full $Shot"
  }
  "shot" { Shot $Shot }
  "box" { $r = PaneBox; if ($r) { "$($r.X),$($r.Y) $($r.Width)x$($r.Height)" } else { "none" } }
}
