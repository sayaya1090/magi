# Drives the real bridge, because the unit tests deliberately never do.
#
# Magi.Core.Tests speaks to streams so that the framing is measurable without a binary, an IDE or a
# companion — and that leaves one thing unmeasured: whether a real `magi ide-bridge` on this machine
# answers what those tests assume. This script is that check, and it is the only place in this
# client that needs magi installed.
#
#   pwsh tools/smoke.ps1                      # uses magi from PATH
#   pwsh tools/smoke.ps1 -Magi C:\path\magi.exe
#
# It asserts nothing about a companion being up: "not-running" is a correct answer and the most
# likely one. What it checks is that the bridge is reachable, speaks the framing, and names the
# methods this client calls.

param(
    [string] $Magi = "magi",
    [string] $Workspace = (Resolve-Path "$PSScriptRoot\..\..\..").Path
)

$ErrorActionPreference = "Stop"
$failures = 0

function Check($what, $ok, $detail) {
    if ($ok) { Write-Host "  ok   $what" } else { Write-Host "  FAIL $what — $detail"; $script:failures++ }
}

# Written as bytes with no BOM. PowerShell's own pipeline prepends one, and three bytes in front of
# the first line used to be a malformed request — the bridge skips it now, but this script must not
# be the thing that proves it does.
$in = Join-Path ([System.IO.Path]::GetTempPath()) "magi-vs-smoke.jsonl"
$requests = @(
    '{"id":1,"method":"about"}'
    '{"id":2,"method":"activity"}'
    '{"id":3,"method":"nosuchmethod"}'
) -join "`n"
[System.IO.File]::WriteAllText($in, $requests + "`n", (New-Object System.Text.UTF8Encoding($false)))

Write-Host "bridge: $Magi ide-bridge -workspace $Workspace"
$raw = & cmd /c "`"$Magi`" ide-bridge -workspace `"$Workspace`" < `"$in`""
$lines = @($raw | Where-Object { $_.Trim() -ne "" })

Check "one reply per request" ($lines.Count -eq 3) "got $($lines.Count) lines"
if ($lines.Count -lt 3) { Remove-Item $in -Force; exit 1 }

$about = $lines[0] | ConvertFrom-Json
$activity = $lines[1] | ConvertFrom-Json
$unknown = $lines[2] | ConvertFrom-Json

Check "about answers"            ($about.ok -eq $true)          "$($lines[0])"
Check "about names its version"  (-not [string]::IsNullOrWhiteSpace($about.version)) "no version"
Check "about reports the socket" (-not [string]::IsNullOrWhiteSpace($about.socket))  "no socket"
foreach ($m in @("about", "activity", "daemon")) {
    Check "about advertises $m" ($about.methods -contains $m) "methods = $($about.methods -join ',')"
}
# Null and an empty list are different facts, and about must never fold them: null is "we could not
# ask", [] is "it named none".
Check "daemon is null or advertised" ($null -eq $about.daemon -or $null -ne $about.daemon.proto) "$($about.daemon)"
if ($null -eq $about.daemon) {
    Check "and says why nobody was home" (-not [string]::IsNullOrWhiteSpace($about.why)) "no why"
}

# "attached", not "idle" — the bridge stopped saying the latter, and this list said it for a day
# longer. Against a companion that was merely running, this check would have failed here.
$words = @("not-running", "attached", "working", "waiting", "unknown")
Check "activity answers in the vocabulary" ($words -contains $activity.state) "state = $($activity.state)"
Check "activity carries a reason when it found nothing" `
    ($activity.state -ne "not-running" -or -not [string]::IsNullOrWhiteSpace($activity.why)) "no why"

Check "an unknown method is named, not ignored" `
    ($unknown.ok -eq $false -and "$($unknown.error)".Contains("nosuchmethod")) "$($lines[2])"

# The trap this client inherits: on Windows the default socket lands under %AppData%, where AF_UNIX
# binds succeed and every connect is refused. Worth saying out loud rather than leaving somebody to
# find it as a timeout.
# No $IsWindows here: that variable does not exist in Windows PowerShell 5.1, so the test would be
# silently false on exactly the platform the warning is for. The path itself says enough.
if ("$($about.socket)" -match '(^|\\)AppData(\\|$)') {
    # ASCII on purpose: the console this runs in is often on a codepage that turns a dash into mojibake.
    Write-Host "  warn the socket is under %AppData% - set MAGI_SOCKET_DIR outside it (DESIGN.ko.md 5)"
}

Remove-Item $in -Force
if ($failures -gt 0) { Write-Host "`n$failures check(s) failed"; exit 1 }
Write-Host "`nall checks passed"
