# orOS Portable

Run [orOS](https://useoros.online) from a USB stick on any Windows or Linux PC, with or without internet. MIT, free forever.

## Which edition?

| | **Full** (`orOS-Portable`) | **Lite** (`orOS-Portable-lite`) |
|---|---|---|
| Size | ~455 MB download, 1–1.5 GB on the stick | ~36 MB |
| Browser | bundled ungoogled-chromium (Windows + Linux) | an installed Chromium, Brave, Edge or Chrome |
| Works on | any Windows 10/11 or 64-bit Linux PC | Windows (Edge); Linux only with a Chromium-family browser |
| Google / Microsoft | none | often runs inside Edge or Chrome |

Not sure? Take the full edition. Both run from the stick and keep data on it. Stick: 32 GB or more recommended (64 GB ideal), 8 GB minimum, formatted as NTFS. The release page has the same table in Greek.

## How it works

The stick holds a small launcher (`orOS.exe` / `orOS`), the orOS web app (`app/`) and, in the full edition, a portable [ungoogled-chromium](https://github.com/ungoogled-software/ungoogled-chromium) (`browser/`). The launcher:

1. serves `app/` from `http://localhost:47800/` (loopback only, read-only, other Host names refused);
2. opens the browser in app mode with its profile in `data/profile/` on the stick;
3. stops the server when the window closes.

The address never changes, because the browser ties stored data to it. When port 47800 is taken at start (`launcher/portbusy.go`), the launcher waits up to 10 seconds for an orOS that is closing, joins an orOS that answers (window or tray), offers to close an earlier orOS launcher that holds the port but no longer answers, and otherwise names the program that holds it (Windows: `GetExtendedTcpTable`; Linux: `/proc`), orOS Drive waiting for a Dropbox sign-in, or a port range Windows reserved for itself (Hyper-V, WSL, Docker; `net stop winnat` / `net start winnat` frees it). orOS's service worker caches the whole app, so it works offline like the installed PWA. Encrypted Dropbox sync works as on useoros.online when online.

The lite edition has no bundled browser and uses an installed Chromium, Brave, Edge or Chrome, with a separate `data/profile-system/` profile (a profile from a newer Chromium may not open in an older one). If the bundled browser fails to start (for example a Linux system that blocks its sandbox), the launcher falls back to an installed one.

### System in Quick settings (full edition and installers)

The full edition and the installers carry orOS USB's System extension (`system/`, fetched from [oros-usb](https://github.com/koulaxizis/oros-usb) `system/extension` by `scripts/build.sh`). The bundled browser loads it with `--load-extension` (Edge and Chrome refuse that flag, so the lite edition and installed browsers go without), and the launcher itself is its native messaging host `online.useoros.system` (`launcher/syshost.go`): Chromium starts `orOS chrome-extension://…/` for each message. On Linux the host manifest lives in the profile (`NativeMessagingHosts/`); on Windows in the data folder with a key under `HKCU\Software\Chromium\NativeMessagingHosts`, which a portable copy takes away again when orOS closes.

The extension then shows in orOS's Quick settings what this computer has (the `caps` in `status`): sound and mute (PipeWire/PulseAudio; Windows Core Audio), screen brightness (logind backlight; WMI on laptops), battery, sleep, restart and shut down of the computer (each confirmed), leaving orOS, full screen or a window, a report zip in Downloads, and, on a stick, "Update orOS" (downloaded by a detached `orOS update-stage` into `app.next`, swapped in at the next start). The stick opens orOS in full screen and the installed apps in a window; the choice is kept in `data/session.json`. Short runs (the host, the update download) log to `system.log` so the window's `launcher.log` stays whole.

Internet window (`launcher/sysweb.go`): the **Internet** button next to the System button opens a normal window of the bundled browser (tabs, address bar, DuckDuckGo as the start page) with its own profile in `data/web`, so sites never share storage with orOS, and uBlock Origin Lite (`ubol/`, fetched with oros-usb's `scripts/ubol.sh`). The lite edition has no button: an installed browser is already at hand.

Wi-Fi and Bluetooth (`launcher/sysnet.go`): Wi-Fi is controlled inside orOS as on the stick (networks, password, radio on or off), through NetworkManager's `nmcli` on Linux and the Native Wi-Fi API (`wlanapi.dll`, `net_windows.go`) on Windows. A network Windows knows connects with its saved profile; a new one gets a WPA2/WPA3 personal or open profile; enterprise networks stay with Windows' settings. Passwords are never logged. Bluetooth opens the computer's own settings (`ms-settings:bluetooth`; GNOME, KDE, Cinnamon, Blueman, Blueberry or Xfce on Linux).

orOS Bridge (`launcher/bridge.go`): the full edition and the installers carry [orOS Bridge](https://github.com/koulaxizis/oros-bridge) (`bridge/<os>/`, the latest release, checked against its `SHA256SUMS.txt` by `scripts/build.sh`; a release build needs 0.8.0 or newer), so orOS Studio can use the computer's VST3 and CLAP plugins and its sound card (ASIO® on Windows, JACK, PipeWire, PulseAudio, ALSA on Linux) without a separate download. The launcher that serves orOS (the window, or the tray of an installed copy) starts it with `--bundled --allow-origin http://localhost:47800 --code-file <data>/bridge-code`: no window, no console, no updates of its own (`oros-bridge.bundled` lies beside it; a new orOS Portable brings the new bridge), its output in `bridge.log`, and it ends with the server (Linux: `Pdeathsig`; Windows: a job object, so it also ends when the launcher is killed). When a bridge already answers on 127.0.0.1:47621 (one the person installed), that one is used. The pairing code appears over orOS as on orOS USB: the System host answers `ping` with `bridgeCode` and `bridge.code` with the open code. `OROS_NO_BRIDGE=1` starts none. The ASIO module (`oros-bridge-asio.dll`) is GPL-3.0; its licence, sources and the written offer are in `LICENSES/`, `SOURCES.txt` and `WRITTEN-OFFER.txt`. ASIO is a registered trademark of Steinberg Media Technologies GmbH.

<img src="https://raw.githubusercontent.com/koulaxizis/oros-bridge/main/asio/logo/asio-compatible-bw.png" alt="ASIO Compatible" height="60">

White-screen watchdog (`launcher/watchdog.go`): once orOS has painted, the extension pings every 10 s. Where it has pinged before, a main window with no ping within 90 s is ended and started once more with `--disable-gpu`.

### Computer files ("This computer")

On [orOS USB](https://github.com/koulaxizis/oros-usb) (the live stick and orOS installed on a disk) the launcher also gives the orOS Files app the computer's own files: the home folder and the disks opened under `/media/oros` (`launcher/computerfs.go`, API under `/__oros/fs/`). On orOS USB (`OROS_COMPUTER_FILES=1`, set by its session) it is always on. orOS Portable and the desktop apps offer the same on Linux once the user agrees for this computer (`launcher/fsconsent.go`: a stick asks once per computer, the desktop apps are on by default, both can switch it off; the answer is kept in `data/computer-files.json` under a hash of the computer's machine ID). There the disks are the ones udisks opens under `/run/media/<user>` or `/media/<user>`, with the launcher as its own disk helper (`orOS disks`, `orOS mount --dev /dev/sdX1 [--unmount]`, `launcher/hostdisks_linux.go`): partitions of 1 GiB or more with a readable filesystem that the running system does not use; the computer's own NTFS (Windows) disks open read-only. On Windows (`launcher/computerfs_windows.go`) the disks are the fixed and removable drives as `/disks/C`, `/disks/D` and so on (named "Label (C:)"), all open already; the system drive is whole and writable, but Windows' own folders at its top (Windows, Program Files, ProgramData, Recovery and the like) and every file or folder marked hidden or system stay out of sight, and names Windows cannot hold (`<>:"|?*`, a trailing dot or space, CON, NUL, COM1 and the like) are refused. Removing moves to the Windows Recycle Bin (a removable drive has none: 409 `norecycle`); orOS lists no Recycle Bin items itself, `POST op {"op": "recyclebin"}` opens it in Explorer. "Unmount" points to Windows' "Safely remove". The orOS Portable folder and the data folder are never reachable. `GET consent` says `on`, `off` or `ask`; `POST consent {"on": true|false}` keeps the answer; every other call answers 404 `off` until then. `OROS_NO_COMPUTER_FILES=1` turns all of it off.

- **Only orOS may call it:** a random key in an `HttpOnly`, `SameSite=Strict` cookie (new at every start, sent with orOS's pages), `Sec-Fetch-Site: same-origin`, the same Origin, and the `X-Oros-FS: 1` header on everything but plain file reads. Other sites, other localhost ports and the Internet window (another profile) are refused.
- **Only the user's files:** `/home/...` is the home folder without names starting with `.` and without `~/oros` (the launcher and browser profiles); `/disks/<name>/...` is a disk while it is mounted. No symbolic link leads out of its place or to a hidden name.
- **Nothing is lost:** writes go to a hidden temporary file, are flushed and renamed; `replace=1` is needed to overwrite and `ifmtime` refuses a file changed elsewhere (412). Removing moves to the freedesktop.org Trash (the home Trash, or the disk's own `.Trash-<uid>`), from where items can be restored or emptied. A move to another disk copies, reads every file back against its SHA-256, and only then trashes the original. A disk is not unmounted while a copy to it runs.
- **Nothing runs:** files are served with `Content-Security-Policy: sandbox` and web pages as plain text. Disks are listed and mounted by the helper in `OROS_FS_HELPER` (`oros-system disks`, `sudo -n oros-system mount --dev /dev/sdX1 [--unmount]`).

| Call | |
|---|---|
| `GET roots` | home and mounted disks, with free and total space and read-only; `os` (`linux` or `windows`) |
| `GET list?path=`, `GET stat?path=` | folder entries `{name, path, dir, size, mtime}` |
| `GET raw?path=` | the file (Range requests for video and audio); `X-Oros-MTime` |
| `PUT raw?path=[&replace=1][&ifmtime=ms]` | write the request body |
| `POST op` `{op, path, to, root, id, dev}` | `mkdir`, `rename`, `copy`, `move`, `trash`, `restore`, `purge`, `mount`, `unmount`, `recyclebin` (Windows) |
| `GET trash`, `GET disks` | the Trash items, the disks that can be opened |

## Installers

The same launcher and browser also install on a computer (`scripts/build-installers.sh`, built in CI next to the portable zips):

| | File | Installs to |
|---|---|---|
| Windows 10/11 | `orOS-Setup-<version>.exe` (Inno Setup, `packaging/windows/orOS.iss`), per user, no admin rights, Start menu entry, uninstaller; offers [orOS Drive](https://github.com/koulaxizis/oros-drive) too (see below) | `%LOCALAPPDATA%\Programs\orOS` |
| Linux | `orOS-<version>-x86_64.AppImage` (+ `.zsync` for AppImageUpdate), adds its own menu entry on start (`remove-menu` takes it away) | runs from the file |
| Debian/Ubuntu | `oros_<version>_amd64.deb`, `/usr/bin/oros`, desktop entry, AppArmor rule that lets the bundled Chromium use its sandbox on Ubuntu 23.10+ | `/opt/oros` |

An `installed` file next to the launcher switches it to installed mode: the browser profile, the log and app updates go to the user's data folder (`%LOCALAPPDATA%\orOS` or `~/.local/share/oros`, `OROS_DATA_DIR` overrides it), and the launcher serves whichever is newer by `APP_VERSION`, the app the installer brought or the one `update` fetched. The address stays `http://localhost:47800/`, but the profile is separate from a stick's, so data moves between them through Dropbox sync. `scripts/test-installers.sh` and `scripts/test-windows-setup.ps1` install, run and remove each package in CI.

Installed copies update themselves (`launcher/autoupdate.go`; portable copies keep the manual `update`). At most every 6 hours, while orOS is open, the launcher checks GitHub in the background: a new commit on orOS `main` is downloaded into `app.next` and swapped in at the next start, never under an open window. A new release of this repo is downloaded too and checked against its `.sha256` asset: the Windows setup then runs silently at the next start (`/RELAUNCH=1` opens orOS again), the AppImage replaces its own file, the .deb is offered at the next start and installed with `pkexec apt-get install` (the desktop's own password prompt), and a Scoop install leaves new releases to `scoop update`. The Windows setup asks whether to update automatically and writes a `no-auto-update` file in the data folder when you say no; that file, or `OROS_NO_AUTO_UPDATE`, turns it off. Test builds (`0.0.0-<sha>`) never update.

### orOS Drive in the Windows setup

The Windows setup carries the latest [orOS Drive](https://github.com/koulaxizis/oros-drive) setup (CI downloads it from that repo's latest release and checks it against its `SHA256SUMS.txt`) and offers "Also install orOS Drive" (unticked) while Drive is not installed. Ticked, it runs Drive's own setup silently for the current user and opens Drive's settings page for the sign-in. Drive stays its own program: its own folder, Start menu entry, tray icon (the orOS mark in reverse colours, gold tile), updates and uninstaller; removing orOS keeps Drive. Once Drive is installed the option is not shown again, so an automatic update of orOS never puts an older Drive back. Linux keeps the separate Drive packages.

### Tray: add or search (installed copies)

`orOS tray` puts the orOS icon in the system tray (`launcher/tray.go`). A click opens a small window, centred on the screen, with orOS's command bar (`http://localhost:47800/__oros-quick#quick-win`, which the server sends on to `/`): type a task and press Enter, or search your orOS data; Escape closes the window. The menu has Add or search, Open orOS, Start with the computer (off until you turn it on: `HKCU\...\Run` value `orOS` on Windows, `~/.config/autostart/oros-tray.desktop` on Linux) and Quit. On Windows, Ctrl+Alt+Shift+Space opens the small window from anywhere. The tray keeps the loopback server up, never reads orOS data (it stays in the browser profile) and runs once per user (`tray.lock` in the data folder). It applies a downloaded orOS, or hands over to a new program version, only while no orOS window is open.

Window placement (`launcher/window.go`): the main orOS window opens maximised and the small one centred at 640×460. The small window has its own address so the browser keeps its size apart from the main window's; before a browser starts on the profile, the launcher sets only the main window's saved placement in the profile's `Default/Preferences` to maximised (it never writes there while the browser runs, and leaves a file it cannot read alone). On Windows the launcher places the small window in the middle of the screen; where it cannot (Linux, or a browser that runs already), the orOS page centres its own window. Known limit on Linux: a main window opened while the small one is open takes the place last saved, not maximised.

`orOS quick` opens the same small window from a command: the Start menu entry "orOS Add or search", the "Add or search" action of the Linux menu entry, or a keyboard shortcut you set in your desktop's settings (Wayland gives apps no global shortcut). Plain GNOME shows no tray icons without the AppIndicator extension; `quick` works there too. The Windows setup offers "Start orOS in the tray with Windows" (unticked) and its uninstaller stops the tray and removes the Run value.

### Install channels

- **Scoop:** `scoop bucket add oros https://github.com/koulaxizis/oros-portable` then `scoop install oros`. The release workflow writes `bucket/oros.json` (from `packaging/scoop/oros.json.in`) to `main`.
- **winget:** `winget install koulaxizis.orOS`. The release workflow submits the manifests (`packaging/winget/`) to microsoft/winget-pkgs when the `WINGET_TOKEN` secret holds a classic token with `public_repo` scope.
- An apt repository is not offered: the .deb (about 185 MB, mostly the browser) is above GitHub Pages' 100 MB file limit, so the .deb updates itself instead.

## Commands

| | |
|---|---|
| `orOS` | start orOS |
| `orOS update` | download the latest orOS from GitHub into `app/` (previous kept as `app.old/`) |
| `orOS serve` | only run the server, for debugging |
| `orOS remove-menu` | remove the menu entry that the AppImage added (and its start at login) |
| `orOS tray` | installed copies: the tray icon (see Tray) |
| `orOS quick` | the small "Add or search" window |
| `orOS update-stage` | download orOS into `app.next` for the next start (the System panel's "Update orOS") |

`OROS_PORTABLE_BROWSER` and `OROS_PORTABLE_BROWSER_ARGS` override the browser and add flags (used by tests).

## Building

`scripts/build.sh <version>` builds both editions into `out/`, `scripts/build-sticktest.sh` the stick test, and `scripts/fetch-sources.sh` collects the source archives for a release. CI (`.github/workflows/build.yml`) runs the Go tests on every push and PR, uploads the zips as a workflow artifact on `main`, and publishes a GitHub release for `v*` tags. Every download in a release has a `.sha256` beside it (`orOS-Portable-<version>.zip.sha256`, `orOS-Portable-lite-<version>.zip.sha256`, and the same for the setup, AppImage and .deb): `sha256sum -c orOS-Portable-<version>.zip.sha256` on Linux, or compare `Get-FileHash orOS-Portable-<version>.zip` with the file on Windows.

## Making sticks

orOS stays free; a stick only saves people the download and setup. Every
build carries `LICENSES/`, `SOURCES.txt` (the exact source of every program)
and `WRITTEN-OFFER.txt`, and a tagged release attaches the third-party source
archives, so copies sold on sticks meet the source terms of the GPL/LGPL
parts inside ungoogled-chromium.

- `sudo scripts/make-sticks.sh orOS-Portable-<version>.zip /dev/sdb /dev/sdc ...`
  first checks the zip against the `.sha256` beside it (when there is one),
  erases each stick, makes one NTFS partition named `OROS`, copies the full
  edition, then re-reads every file from the stick and checks it against
  `SHA256SUMS.txt`. It writes all sticks in parallel (a powered USB hub helps)
  and refuses anything that is not a removable USB disk up to 256 GB, or that
  holds a system mount. It asks you to type `ERASE` first. Linux only; needs
  `ntfs-3g`. `scripts/test-make-sticks.sh` tests it on loop devices (CI).
- `orOS-sticktest` (release asset, Windows and Linux) checks whether a stick
  is fast enough: copy it onto the stick and run it there. It measures
  sequential write/read, random 4 KiB writes that wait for the disk (what the
  browser profile does all day) and small-file creation, gives a verdict, and
  appends the numbers to `sticktest-results.txt` on the stick. Use it to pick a
  stick model from samples; it does not measure how long the flash will last.

## Notes

- Private browsing: put a VPN extension in the computer's own browser, not in orOS; see [Private browsing](PRIVATE-BROWSING.md) (EN/EL).
- Format the stick as NTFS. On Linux the browser locks its profile with symbolic links, which exFAT and FAT32 cannot store (the launcher then says so), and FAT32 also mounts without execute permission. Windows runs from any of them.
- The Windows launcher is not code-signed, so SmartScreen warns on first run ("More info" → "Run anyway").
- Close the orOS window before removing the stick; pulling it out while the browser writes can corrupt the profile.
- Dropbox sign-in returns to `http://localhost:47800/`, which must be listed as a redirect URI in the orOS Dropbox app.

## Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org) (applied for; until it is granted the Windows files are unsigned and SmartScreen warns once).

- Committers and reviewers: [Chris (koulaxizis)](https://github.com/koulaxizis)
- Approvers: [Chris (koulaxizis)](https://github.com/koulaxizis)

Signed: the orOS launcher (`orOS.exe`) and the Windows setup (`orOS-Setup-*.exe`), built from this repository by GitHub Actions. The bundled ungoogled-chromium is included unmodified and is not signed by us.

Privacy: the installed orOS checks GitHub for updates (the orOS web app and new releases of this repository) at most every 6 hours; the setup lets you turn this off. It sends nothing else anywhere unless you ask it to (for example Dropbox sync). See the [privacy policy](https://useoros.online/privacy.html).

## Credits and licences

orOS Portable: MIT. It stands on the work of:

- [ungoogled-chromium](https://github.com/ungoogled-software/ungoogled-chromium) and its contributors, the browser in the full edition and the installers: BSD-3-Clause.
- [Chromium](https://www.chromium.org/) by The Chromium Authors, which ungoogled-chromium is built from: BSD-3-Clause, with LGPL, MPL, Apache-2.0, MIT and other open-source components (full list at `chrome://credits` in the bundled browser).
- [Go](https://go.dev/) by The Go Authors, the launcher's language and standard library: BSD-3-Clause.
- [Inno Setup](https://jrsoftware.org/isinfo.php) by Jordan Russell and Martijn Laan, the Windows setup (Inno Setup License); Greek texts from Inno Setup's translation by Anastasis Chatzioglou, XhmikosR and Vasileios Karamichail.
- [appimagetool](https://github.com/AppImage/appimagetool) and the [AppImage runtime](https://github.com/AppImage/type2-runtime) (MIT, linked with libfuse under LGPL-2.1, squashfuse, zstd, zlib, mimalloc and musl), the Linux AppImage.
- The orOS web app's libraries, fonts, data and services are credited in [CREDITS.md](https://github.com/koulaxizis/oros/blob/main/CREDITS.md) of the orOS repository (`app/CREDITS.md` in every build).

Every build carries `LICENSES/` (`CREDITS.txt`, orOS and orOS Portable MIT, `Go-BSD.txt`; the full edition also `ungoogled-chromium-BSD.txt` and `Chromium-BSD.txt`; the AppImage also `AppImage-runtime.txt` and `LGPL-2.1.txt`), `SOURCES.txt` with the exact source of every program, and `WRITTEN-OFFER.txt`. The .deb's `/usr/share/doc/oros/copyright` points to them.
