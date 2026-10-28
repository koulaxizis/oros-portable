# Installs orOS-Setup.exe silently for the current user, checks that the
# installed launcher serves orOS with its data in %LOCALAPPDATA%\orOS,
# then uninstalls it silently and checks that the data was kept.
param([Parameter(Mandatory)] [string] $Setup)
$ErrorActionPreference = 'Stop'

# "autostart" and "drive" are off by default; the test turns them on to
# check the Run value and the bundled orOS Drive setup.
$p = Start-Process $Setup -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CURRENTUSER', '/MERGETASKS=autostart,drive', '/LOG=setup.log' -Wait -PassThru
if ($p.ExitCode) { Get-Content setup.log -Tail 30; throw "setup exited with $($p.ExitCode)" }

$app = Join-Path $env:LOCALAPPDATA 'Programs\orOS'
$data = Join-Path $env:LOCALAPPDATA 'orOS'
foreach ($f in 'orOS.exe', 'installed', 'app\index.html', 'browser\windows\chrome.exe', 'unins000.exe') {
  if (-not (Test-Path (Join-Path $app $f))) { throw "missing after install: $f" }
}
$driveApp = Join-Path $env:LOCALAPPDATA 'Programs\orOS Drive'
if (-not (Test-Path (Join-Path $driveApp 'orOS Drive.exe'))) { Get-Content setup.log -Tail 30; throw 'orOS Drive was not installed' }
Write-Host 'ok: orOS Drive installed by the orOS setup'

# Like an automatic update: Drive is installed now, so the setup does not
# run its bundled Drive setup again (that could put an older Drive back).
$driveExe = Get-Item (Join-Path $driveApp 'orOS Drive.exe')
$p = Start-Process $Setup -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CURRENTUSER', '/LOG=setup2.log' -Wait -PassThru
if ($p.ExitCode) { Get-Content setup2.log -Tail 30; throw "second setup exited with $($p.ExitCode)" }
if (Select-String -Path setup2.log -Pattern 'orOS-Drive-Setup' -Quiet) { throw 'the second setup ran the Drive setup again' }
if ((Get-Item (Join-Path $driveApp 'orOS Drive.exe')).LastWriteTimeUtc -ne $driveExe.LastWriteTimeUtc) { throw 'orOS Drive was replaced' }
Write-Host 'ok: a second run leaves orOS Drive alone'

$menu = Join-Path ([Environment]::GetFolderPath('Programs')) 'orOS.lnk'
if (-not (Test-Path $menu)) { throw "no Start menu entry: $menu" }

$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$run = (Get-ItemProperty $runKey -ErrorAction SilentlyContinue).orOS
if ($run -ne ('"' + (Join-Path $app 'orOS.exe') + '" tray')) { throw "Run value is '$run'" }
if (-not (Test-Path (Join-Path ([Environment]::GetFolderPath('Programs')) 'orOS Add or search.lnk'))) { throw 'no "Add or search" Start menu entry' }
Write-Host 'ok: start with Windows (tray) and the Add or search entry'

$srv = Start-Process (Join-Path $app 'orOS.exe') -ArgumentList 'serve' -PassThru
try {
  $ok = $false
  for ($i = 0; $i -lt 30 -and -not $ok; $i++) {
    try { $r = Invoke-WebRequest http://localhost:47800/ -UseBasicParsing; $ok = $r.StatusCode -eq 200 } catch { Start-Sleep 1 }
  }
  if (-not $ok) { throw 'the installed launcher does not serve http://localhost:47800/' }
  if ($r.Content -notmatch '<title>orOS') { throw 'the page is not orOS' }
  $js = (Invoke-WebRequest http://localhost:47800/shell.js -UseBasicParsing).Content
  if ($js -notmatch 'APP_VERSION = "') { throw 'shell.js has no APP_VERSION' }
  if (-not (Test-Path (Join-Path $data 'launcher.log'))) { throw "no data folder: $data" }
  Write-Host "ok: installed orOS serves, data in $data"
} finally {
  Stop-Process -Id $srv.Id -Force -ErrorAction SilentlyContinue
}
Start-Sleep 2

# The tray keeps serving orOS; a second tray stops at its lock; the
# uninstaller must stop the tray, or orOS.exe could not be removed.
$tray = Start-Process (Join-Path $app 'orOS.exe') -ArgumentList 'tray' -PassThru
$ok = $false
for ($i = 0; $i -lt 30 -and -not $ok; $i++) {
  try { $ok = (Invoke-WebRequest http://localhost:47800/ -UseBasicParsing).StatusCode -eq 200 } catch { Start-Sleep 1 }
}
if (-not $ok) { throw 'the tray does not serve http://localhost:47800/' }
$second = Start-Process (Join-Path $app 'orOS.exe') -ArgumentList 'tray' -PassThru
if (-not $second.WaitForExit(15000)) { throw 'a second tray did not stop' }
if ($tray.HasExited) { throw 'the first tray stopped' }
Write-Host 'ok: the tray serves, one per user'

$u = Start-Process (Join-Path $app 'unins000.exe') -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
if ($u.ExitCode) { throw "uninstall exited with $($u.ExitCode)" }
# The uninstaller finishes in a copy of itself, so wait for the files to go.
for ($i = 0; $i -lt 30 -and (Test-Path (Join-Path $app 'orOS.exe')); $i++) { Start-Sleep 1 }
if (Test-Path (Join-Path $app 'orOS.exe')) { throw 'orOS.exe is still there after uninstall' }
if (Test-Path $menu) { throw 'the Start menu entry is still there after uninstall' }
if ((Get-ItemProperty $runKey -ErrorAction SilentlyContinue).orOS) { throw 'the Run value is still there after uninstall' }
if (-not $tray.HasExited) { throw 'the uninstaller left the tray running' }
if (-not (Test-Path (Join-Path $data 'launcher.log'))) { throw 'a silent uninstall must keep the data' }
Write-Host 'ok: uninstalled cleanly, data kept'

# orOS Drive is its own program: removing orOS keeps it; its own
# uninstaller removes it.
if (-not (Test-Path (Join-Path $driveApp 'orOS Drive.exe'))) { throw 'removing orOS removed orOS Drive' }
Get-Process 'orOS Drive' -ErrorAction SilentlyContinue | Stop-Process -Force
$u = Start-Process (Join-Path $driveApp 'unins000.exe') -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART' -Wait -PassThru
if ($u.ExitCode) { throw "Drive uninstall exited with $($u.ExitCode)" }
Write-Host 'ok: orOS Drive kept by the orOS uninstaller, removed by its own'
