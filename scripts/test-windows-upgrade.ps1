# Installs the latest released orOS-Setup.exe, lets it write its data and
# settings, then installs the new setup over it the way the automatic
# update does (launcher/autoupdate.go runPendingSetup) and checks that
# the data, the settings and the start-up survive:
#   - a marker in the data folder and one in the browser profile folder
#     (where orOS keeps its own data) are untouched,
#   - "no-auto-update" (written by the old setup, task autoupdate off) and
#     the Run value (task autostart on) are kept,
#   - the program is the new one (installed, served shell.js), with one
#     uninstall entry, and the tray comes back (/RELAUNCH=tray).
# Run after test-windows-setup.ps1 (which leaves orOS uninstalled).
# Needs gh with GH_TOKEN.
param([Parameter(Mandatory)] [string] $Setup)
$ErrorActionPreference = 'Stop'

$app = Join-Path $env:LOCALAPPDATA 'Programs\orOS'
$data = Join-Path $env:LOCALAPPDATA 'orOS'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$uninstKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\{4AABB5BE-C57D-4A02-891C-400A48085E33}_is1'
$newVer = ((Split-Path $Setup -Leaf) -replace '^orOS-Setup-(.*)\.exe$', '$1')

function Stop-OrOS {
  Get-Process orOS, chrome, oros-bridge, 'orOS Drive', orosdrive -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -like "$app\*" -or $_.Path -like "$env:LOCALAPPDATA\Programs\orOS Drive\*" } | Stop-Process -Force
  for ($i = 0; $i -lt 15; $i++) {
    try { Invoke-WebRequest http://localhost:47800/ -UseBasicParsing -TimeoutSec 2 | Out-Null; Start-Sleep 1 } catch { break }
  }
}
function Wait-Serves([string] $what) {
  for ($i = 0; $i -lt 45; $i++) {
    try { if ((Invoke-WebRequest http://localhost:47800/ -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200) { return } } catch { Start-Sleep 1 }
  }
  Get-Content (Join-Path $data 'launcher.log'), (Join-Path $data 'tray.log') -Tail 20 -ErrorAction SilentlyContinue
  throw "$what does not serve http://localhost:47800/"
}
# Test-Marks <when>: both markers there and unchanged, else show what the data folder holds.
function Test-Marks([string] $when) {
  foreach ($m in $marks) {
    $why = if (-not (Test-Path $m)) { 'lost' } elseif ((Get-Content $m -Raw) -ne "kept from orOS $oldVer") { 'changed' } else { '' }
    if ($why) {
      Write-Host "data folder ($when):"
      Get-ChildItem $data -Recurse -Depth 2 -ErrorAction SilentlyContinue | Select-Object -First 60 | ForEach-Object { "  $($_.FullName) $($_.LastWriteTimeUtc.ToString('HH:mm:ss'))" }
      Get-Content upgrade-new.log -Tail 40 -ErrorAction SilentlyContinue
      Get-Content (Join-Path $data 'launcher.log'), (Join-Path $data 'tray.log') -Tail 30 -ErrorAction SilentlyContinue
      throw "$why $($when): $m"
    }
  }
}
function AppVersion([string] $js) { if ($js -match 'APP_VERSION = "([^"]+)"') { $Matches[1] } else { '' } }

# A clean start: no orOS, no data, no Run value from the tests before.
if (Test-Path (Join-Path $app 'orOS.exe')) { throw 'orOS is still installed before the upgrade test' }
Stop-OrOS
Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
Remove-ItemProperty $runKey -Name orOS -ErrorAction SilentlyContinue

# The latest release (what people have installed), checked against its .sha256.
$old = Join-Path $env:RUNNER_TEMP 'previous-setup'
New-Item -ItemType Directory -Force $old | Out-Null
gh release download --repo $env:GITHUB_REPOSITORY --dir $old --clobber --pattern 'orOS-Setup-*.exe' --pattern 'orOS-Setup-*.exe.sha256'
if ($LASTEXITCODE) { throw 'could not download the latest orOS-Setup.exe' }
$oldSetup = Get-ChildItem $old -Filter 'orOS-Setup-*.exe' | Select-Object -First 1
$want = ((Get-Content "$($oldSetup.FullName).sha256" -Raw).Trim() -split '\s+')[0]
if ((Get-FileHash $oldSetup.FullName -Algorithm SHA256).Hash.ToLower() -ne $want.ToLower()) { throw "$($oldSetup.Name) does not match its .sha256" }
$oldVer = ($oldSetup.Name -replace '^orOS-Setup-(.*)\.exe$', '$1')
Write-Host "upgrade: orOS $oldVer -> $newVer"

# Old setup: start with Windows on, automatic updates off (so nothing
# else changes the program while the test runs), no orOS Drive.
$p = Start-Process $oldSetup.FullName -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CURRENTUSER', '/TASKS=autostart', '/LOG=upgrade-old.log' -Wait -PassThru
if ($p.ExitCode) { Get-Content upgrade-old.log -Tail 30; throw "the $oldVer setup exited with $($p.ExitCode)" }
if (-not (Test-Path (Join-Path $data 'no-auto-update'))) { throw "the $oldVer setup did not write no-auto-update" }
$run = (Get-ItemProperty $runKey -ErrorAction SilentlyContinue).orOS
if (-not $run) { throw "the $oldVer setup did not write the Run value" }

# The old orOS runs once and writes its data folder.
$srv = Start-Process (Join-Path $app 'orOS.exe') -ArgumentList 'serve' -PassThru
try { Wait-Serves "orOS $oldVer" } finally { Stop-Process -Id $srv.Id -Force -ErrorAction SilentlyContinue }
Stop-OrOS
if (-not (Test-Path (Join-Path $data 'launcher.log'))) { throw "orOS $oldVer wrote no data folder" }
$marks = @((Join-Path $data 'upgrade-marker.txt'), (Join-Path $data 'profile\Default\upgrade-marker.txt'))
foreach ($m in $marks) {
  New-Item -ItemType Directory -Force (Split-Path $m) | Out-Null
  [IO.File]::WriteAllText($m, "kept from orOS $oldVer")
  if (-not [IO.File]::Exists($m)) { throw "could not write $m" }
}
Start-Sleep 3
if (-not (Test-Path $marks[0]) -or -not (Test-Path $marks[1])) {
  # something on this computer removes files from the data folder: show who runs there
  Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*orOS*' -or $_.ExecutablePath -like '*orOS*' } |
    ForEach-Object { "  $($_.ProcessId) $($_.Name): $($_.CommandLine)" }
}
Test-Marks 'three seconds after writing them'
$noAuto = Get-Content (Join-Path $data 'no-auto-update') -Raw

# The new setup, with the automatic update's own arguments.
# Not Start-Process -Wait: that also waits for the tray the setup starts
# again, which keeps running.
$p = Start-Process $Setup -ArgumentList '/SILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CURRENTUSER', '/RELAUNCH=tray', '/LOG=upgrade-new.log' -PassThru
if (-not $p.WaitForExit(600000)) { Get-Content upgrade-new.log -Tail 30 -ErrorAction SilentlyContinue; throw 'the new setup did not finish in 10 minutes' }
if ($p.ExitCode) { Get-Content upgrade-new.log -Tail 30; throw "the new setup over $oldVer exited with $($p.ExitCode)" }

Test-Marks 'by the setup'

# The tray it started again serves the new orOS.
Wait-Serves 'the tray after the upgrade'
$installed = Get-Content (Join-Path $app 'installed') -Raw
if ($installed -notmatch [regex]::Escape("orOS $newVer,")) { throw "installed says: $installed" }
$served = AppVersion (Invoke-WebRequest http://localhost:47800/shell.js -UseBasicParsing).Content
$bundled = AppVersion (Get-Content (Join-Path $app 'app\shell.js') -Raw)
if (-not $served -or $served -ne $bundled) { throw "serves orOS $served, the new setup carries $bundled" }
Write-Host "ok: the tray came back and serves the new orOS ($served)"

Test-Marks 'after the new orOS started'
Write-Host 'ok: the data folder and the browser profile were kept'

if ((Get-Content (Join-Path $data 'no-auto-update') -Raw -ErrorAction SilentlyContinue) -ne $noAuto) { throw 'the upgrade turned automatic updates back on' }
if ((Get-ItemProperty $runKey -ErrorAction SilentlyContinue).orOS -ne $run) { throw 'the upgrade changed or removed the Run value' }
if (Test-Path (Join-Path $env:LOCALAPPDATA 'Programs\orOS Drive\orOS Drive.exe')) { throw 'a silent upgrade added orOS Drive' }
$entry = Get-ItemProperty $uninstKey -ErrorAction SilentlyContinue
if (-not $entry -or $entry.DisplayVersion -ne $newVer) { throw "Add or remove programs shows '$($entry.DisplayVersion)', not $newVer" }
if (@(Get-ChildItem 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall' | Where-Object { $_.GetValue('DisplayName') -eq 'orOS' }).Count -ne 1) { throw 'more than one orOS in Add or remove programs' }
Write-Host 'ok: settings kept (automatic updates off, start with Windows on), one orOS in Add or remove programs'

Stop-OrOS
$u = Start-Process (Join-Path $app 'unins000.exe') -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
if ($u.ExitCode) { throw "uninstall after the upgrade exited with $($u.ExitCode)" }
for ($i = 0; $i -lt 30 -and (Test-Path (Join-Path $app 'orOS.exe')); $i++) { Start-Sleep 1 }
if (Test-Path (Join-Path $app 'orOS.exe')) { throw 'orOS.exe is still there after uninstall' }
foreach ($m in $marks) { if (-not (Test-Path $m)) { throw "the uninstall after the upgrade removed $m" } }
Write-Host 'ok: the upgraded orOS uninstalls, data kept'
