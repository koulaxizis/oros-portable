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

The address never changes, because the browser ties stored data to it. orOS's service worker caches the whole app, so it works offline like the installed PWA. Encrypted Dropbox sync works as on useoros.online when online.

The lite edition has no bundled browser and uses an installed Chromium, Brave, Edge or Chrome, with a separate `data/profile-system/` profile (a profile from a newer Chromium may not open in an older one). If the bundled browser fails to start (for example a Linux system that blocks its sandbox), the launcher falls back to an installed one.

## Installers

The same launcher and browser also install on a computer (`scripts/build-installers.sh`, built in CI next to the portable zips):

| | File | Installs to |
|---|---|---|
| Windows 10/11 | `orOS-Setup-<version>.exe` (Inno Setup, `packaging/windows/orOS.iss`), per user, no admin rights, Start menu entry, uninstaller | `%LOCALAPPDATA%\Programs\orOS` |
| Linux | `orOS-<version>-x86_64.AppImage` (+ `.zsync` for AppImageUpdate), adds its own menu entry on start (`remove-menu` takes it away) | runs from the file |
| Debian/Ubuntu | `oros_<version>_amd64.deb`, `/usr/bin/oros`, desktop entry, AppArmor rule that lets the bundled Chromium use its sandbox on Ubuntu 23.10+ | `/opt/oros` |

An `installed` file next to the launcher switches it to installed mode: the browser profile, the log and app updates go to the user's data folder (`%LOCALAPPDATA%\orOS` or `~/.local/share/oros`, `OROS_DATA_DIR` overrides it), and the launcher serves whichever is newer by `APP_VERSION`, the app the installer brought or the one `update` fetched. The address stays `http://localhost:47800/`, but the profile is separate from a stick's, so data moves between them through Dropbox sync. `scripts/test-installers.sh` and `scripts/test-windows-setup.ps1` install, run and remove each package in CI.

Installed copies update themselves (`launcher/autoupdate.go`; portable copies keep the manual `update`). At most every 6 hours, while orOS is open, the launcher checks GitHub in the background: a new commit on orOS `main` is downloaded into `app.next` and swapped in at the next start, never under an open window. A new release of this repo is downloaded too and checked against its `.sha256` asset: the Windows setup then runs silently at the next start (`/RELAUNCH=1` opens orOS again), the AppImage replaces its own file, the .deb is offered at the next start and installed with `pkexec apt-get install` (the desktop's own password prompt), and a Scoop install leaves new releases to `scoop update`. The Windows setup asks whether to update automatically and writes a `no-auto-update` file in the data folder when you say no; that file, or `OROS_NO_AUTO_UPDATE`, turns it off. Test builds (`0.0.0-<sha>`) never update.

### Tray: add or search (installed copies)

`orOS tray` puts the orOS icon in the system tray (`launcher/tray.go`). A click opens a small window with orOS's command bar (`http://localhost:47800/#quick-win`): type a task and press Enter, or search your orOS data; Escape closes the window. The menu has Add or search, Open orOS, Start with the computer (off until you turn it on: `HKCU\...\Run` value `orOS` on Windows, `~/.config/autostart/oros-tray.desktop` on Linux) and Quit. On Windows, Ctrl+Alt+Shift+Space opens the small window from anywhere. The tray keeps the loopback server up, never reads orOS data (it stays in the browser profile) and runs once per user (`tray.lock` in the data folder). It applies a downloaded orOS, or hands over to a new program version, only while no orOS window is open.

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

`OROS_PORTABLE_BROWSER` and `OROS_PORTABLE_BROWSER_ARGS` override the browser and add flags (used by tests).

## Building

`scripts/build.sh <version>` builds both editions into `out/`, `scripts/build-sticktest.sh` the stick test, and `scripts/fetch-sources.sh` collects the source archives for a release. CI (`.github/workflows/build.yml`) runs the Go tests on every push and PR, uploads the zips as a workflow artifact on `main`, and publishes a GitHub release for `v*` tags.

## Making sticks

orOS stays free; a stick only saves people the download and setup. Every
build carries `LICENSES/`, `SOURCES.txt` (the exact source of every program)
and `WRITTEN-OFFER.txt`, and a tagged release attaches the third-party source
archives, so copies sold on sticks meet the source terms of the GPL/LGPL
parts inside ungoogled-chromium.

- `sudo scripts/make-sticks.sh orOS-Portable-<version>.zip /dev/sdb /dev/sdc ...`
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
