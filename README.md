# orOS Portable

Run [orOS](https://useoros.online) from a USB stick on any Windows or Linux PC, with or without internet. MIT, free forever.

## How it works

The stick holds a small launcher (`orOS.exe` / `orOS`), the orOS web app (`app/`) and, in the full edition, a portable [ungoogled-chromium](https://github.com/ungoogled-software/ungoogled-chromium) (`browser/`). The launcher:

1. serves `app/` from `http://localhost:47800/` (loopback only, read-only, other Host names refused);
2. opens the browser in app mode with its profile in `data/profile/` on the stick;
3. stops the server when the window closes.

The address never changes, because the browser ties stored data to it. orOS's service worker caches the whole app, so it works offline like the installed PWA. Encrypted Dropbox sync works as on useoros.online when online.

The lite edition has no bundled browser and uses an installed Chromium, Brave, Edge or Chrome, with a separate `data/profile-system/` profile (a profile from a newer Chromium may not open in an older one). If the bundled browser fails to start (for example a Linux system that blocks its sandbox), the launcher falls back to an installed one.

## Commands

| | |
|---|---|
| `orOS` | start orOS |
| `orOS update` | download the latest orOS from GitHub into `app/` (previous kept as `app.old/`) |
| `orOS serve` | only run the server, for debugging |

`OROS_PORTABLE_BROWSER` and `OROS_PORTABLE_BROWSER_ARGS` override the browser and add flags (used by tests).

## Building

`scripts/build.sh <version>` builds both editions into `out/`. CI (`.github/workflows/build.yml`) runs the Go tests on every push and PR, uploads the zips as a workflow artifact on `main`, and publishes a GitHub release for `v*` tags.

## Notes

- Linux: format the stick as exFAT or NTFS. FAT32 mounts without execute permission, so the launcher cannot run from it.
- The Windows launcher is not code-signed, so SmartScreen warns on first run ("More info" → "Run anyway").
- Close the orOS window before removing the stick; pulling it out while the browser writes can corrupt the profile.
- Dropbox sign-in returns to `http://localhost:47800/`, which must be listed as a redirect URI in the orOS Dropbox app.

## Licences

orOS Portable: MIT. ungoogled-chromium: BSD-3-Clause (its licence files ship inside `browser/`).
