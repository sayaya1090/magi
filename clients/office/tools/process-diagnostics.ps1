# Read-only process inventory. Never prints API keys or complete command lines.
$all = @(Get-CimInstance Win32_Process)
$rows = foreach ($p in $all) {
  if ($p.Name -notmatch '^(magi.*\.exe|POWERPNT\.EXE|WINWORD\.EXE|EXCEL\.EXE)$') { continue }
  $parent = $all | Where-Object ProcessId -eq $p.ParentProcessId | Select-Object -First 1
  $role = 'other'
  if ($p.Name -match '^POWERPNT') { $role = 'PowerPoint' }
  elseif ($p.Name -match '^WINWORD') { $role = 'Word' }
  elseif ($p.Name -match '^EXCEL') { $role = 'Excel' }
  elseif ($p.Name -match 'hand') { $role = 'COM editing adapter' }
  elseif ($p.CommandLine -match '\s+office(?:\s|$)') { $role = 'Office helper' }
  elseif ($p.CommandLine -match '--detach(?:\s|$)') { $role = 'temporary daemon launcher' }
  elseif ($p.CommandLine -match '--daemon(?:\s|$)') { $role = 'companion daemon' }
  elseif ($p.CommandLine -match '--mcp(?:\s|$)') { $role = 'MCP bridge' }
  [pscustomobject]@{ PID=$p.ProcessId; ParentPID=$p.ParentProcessId; Parent=$parent.Name; Name=$p.Name; Role=$role; Started=$p.CreationDate; Executable=$p.ExecutablePath }
}
$rows | Sort-Object Role,PID | Format-Table -AutoSize
