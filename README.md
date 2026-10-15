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

`scripts/build.sh <version>` builds both editions into `out/`, `scripts/build-sticktest.sh` the stick test, and `scripts/fetch-sources.sh` collects the source archives for a release. CI (`.github/workflows/build.yml`) runs the Go tests on every push and PR, uploads the zips as a workflow artifact on `main`, and publishes a GitHub release for `v*` tags.

## Making sticks

orOS stays free; a stick only saves people the download and setup. Every
build carries `LICENSES/`, `SOURCES.txt` (the exact source of every program)
and `WRITTEN-OFFER.txt`, and a tagged release attaches the third-party source
archives, so copies sold on sticks meet the source terms of the GPL/LGPL
parts inside ungoogled-chromium.

- `sudo scripts/make-sticks.sh orOS-Portable-<version>.zip /dev/sdb /dev/sdc ...`
  erases each stick, makes one exFAT partition named `OROS`, copies the full
  edition, then re-reads every file from the stick and checks it against
  `SHA256SUMS.txt`. It writes all sticks in parallel (a powered USB hub helps)
  and refuses anything that is not a removable USB disk up to 256 GB, or that
  holds a system mount. It asks you to type `ERASE` first. Linux only; needs
  `exfatprogs`. `scripts/test-make-sticks.sh` tests it on loop devices (CI).
- `orOS-sticktest` (release asset, Windows and Linux) checks whether a stick
  is fast enough: copy it onto the stick and run it there. It measures
  sequential write/read, random 4 KiB writes that wait for the disk (what the
  browser profile does all day) and small-file creation, gives a verdict, and
  appends the numbers to `sticktest-results.txt` on the stick. Use it to pick a
  stick model from samples; it does not measure how long the flash will last.

## Notes

- Linux: format the stick as exFAT or NTFS. FAT32 mounts without execute permission, so the launcher cannot run from it.
- The Windows launcher is not code-signed, so SmartScreen warns on first run ("More info" → "Run anyway").
- Close the orOS window before removing the stick; pulling it out while the browser writes can corrupt the profile.
- Dropbox sign-in returns to `http://localhost:47800/`, which must be listed as a redirect URI in the orOS Dropbox app.

## Licences

orOS Portable: MIT. ungoogled-chromium: BSD-3-Clause, with LGPL and other open-source components (its licence files ship inside `browser/` and are copied to `LICENSES/`; the full list is at `chrome://credits`). Sources: `SOURCES.txt`, written offer: `WRITTEN-OFFER.txt`.
