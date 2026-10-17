Install [orOS](https://useoros.online) on any Windows or Linux PC, or run it from a USB stick, with or without internet. Free forever, MIT.

## Install on this computer

| | File | |
|---|---|---|
| **Windows 10/11** | `orOS-Setup-<version>.exe` | No administrator rights needed. Adds orOS to the Start menu; remove it from Settings › Apps. |
| **Linux (any)** | `orOS-<version>-x86_64.AppImage` | One file, no install: make it executable and double-click. It adds itself to the app menu. |
| **Debian, Ubuntu, Mint** | `oros_<version>_amd64.deb` | Open it with the software installer, or `sudo apt install ./oros_*.deb`. Best choice on Ubuntu 24.04+. |

The installed orOS uses its own private browser (ungoogled-chromium), works offline, and keeps itself up to date: new orOS versions download in the background and open the next time you start it (Windows setup and AppImage update themselves too; the .deb tells you when a new version is out). Its data stays on this computer (`%LOCALAPPDATA%\orOS` or `~/.local/share/oros`), apart from useoros.online in your browser and from any stick: use Dropbox sync in orOS to carry it over. Windows may say "Windows protected your PC" because the setup is not signed yet: choose "More info" › "Run anyway".

## Run from a USB stick

### Which file do I download?

| | **orOS-Portable** (full) | **orOS-Portable-lite** |
|---|---|---|
| Size | about 455 MB download, 1–1.5 GB on the stick | about 36 MB |
| Browser | its own private browser (ungoogled-chromium), for Windows and Linux | none: uses one installed on the PC (Chromium, Brave, Edge or Chrome) |
| Works on | any Windows 10/11 or 64-bit Linux PC, the same everywhere | Windows (Edge is always there); Linux only if a Chromium-family browser is installed |
| Google / Microsoft | none | often runs inside Edge or Chrome |
| Watch out | — | a PC with an older browser than the last one used may not open your profile |

**Not sure? Take the full edition.** Pick lite only for a small download or a small stick. Both run from the stick and keep your data on it.

**Stick size:** 32 GB or more recommended (64 GB is ideal and usually faster); 8 GB is the minimum. Format the stick as **NTFS** (on Linux, exFAT and FAT32 cannot hold the browser's data), unzip, and copy everything inside the `orOS-Portable` folder to the stick. Then double-click `orOS.exe` (Windows) or run `./orOS` (Linux).

`orOS-sticktest-*`: optional speed test. Copy it onto a stick and run it there to see whether the stick is fast enough. Files ending in `-src` and `SOURCES.txt`: the source code of everything in this release.

---

Εγκαθιστάς το [orOS](https://useoros.online) σε οποιονδήποτε υπολογιστή με Windows ή Linux, ή το τρέχεις από USB stick, με ή χωρίς internet. Δωρεάν για πάντα, MIT.

## Εγκατάσταση στον υπολογιστή

| | Αρχείο | |
|---|---|---|
| **Windows 10/11** | `orOS-Setup-<έκδοση>.exe` | Δεν χρειάζεται διαχειριστής. Μπαίνει στο μενού Έναρξη· αφαιρείται από Ρυθμίσεις › Εφαρμογές. |
| **Linux (κάθε διανομή)** | `orOS-<έκδοση>-x86_64.AppImage` | Ένα αρχείο χωρίς εγκατάσταση: το κάνεις εκτελέσιμο και διπλό κλικ. Μπαίνει μόνο του στο μενού εφαρμογών. |
| **Debian, Ubuntu, Mint** | `oros_<έκδοση>_amd64.deb` | Το ανοίγεις με το κέντρο λογισμικού ή `sudo apt install ./oros_*.deb`. Η καλύτερη επιλογή σε Ubuntu 24.04+. |

Το εγκατεστημένο orOS έχει δικό του ιδιωτικό browser (ungoogled-chromium), δουλεύει χωρίς internet και ενημερώνεται μόνο του: οι νέες εκδόσεις του orOS κατεβαίνουν στο παρασκήνιο και ανοίγουν την επόμενη φορά που θα το ξεκινήσεις (ο εγκαταστάτης των Windows και το AppImage αναβαθμίζονται κι αυτά· το .deb σε ειδοποιεί όταν βγει νέα έκδοση). Τα δεδομένα του μένουν σε αυτόν τον υπολογιστή (`%LOCALAPPDATA%\orOS` ή `~/.local/share/oros`), χωριστά από το useoros.online στον browser σου και από κάθε stick: για να τα μεταφέρεις, χρησιμοποίησε το Dropbox sync του orOS. Τα Windows μπορεί να πουν «Τα Windows προστάτευσαν τον υπολογιστή σας», επειδή ο εγκαταστάτης δεν είναι ακόμα υπογεγραμμένος: «Περισσότερες πληροφορίες» › «Εκτέλεση οπωσδήποτε».

## Από USB stick

### Ποιο αρχείο να κατεβάσω;

| | **orOS-Portable** (πλήρες) | **orOS-Portable-lite** |
|---|---|---|
| Μέγεθος | περίπου 455 MB λήψη, 1–1,5 GB στο stick | περίπου 36 MB |
| Browser | δικός του ιδιωτικός browser (ungoogled-chromium), για Windows και Linux | κανένας: χρησιμοποιεί όποιον είναι εγκατεστημένος (Chromium, Brave, Edge ή Chrome) |
| Δουλεύει σε | κάθε υπολογιστή με Windows 10/11 ή Linux 64-bit, ίδιο παντού | Windows (ο Edge υπάρχει πάντα)· σε Linux μόνο αν υπάρχει browser τύπου Chromium |
| Google / Microsoft | καθόλου | συχνά τρέχει μέσα σε Edge ή Chrome |
| Προσοχή | — | σε υπολογιστή με παλιότερο browser από τον τελευταίο που χρησιμοποίησες, το προφίλ μπορεί να μην ανοίξει |

**Δεν είσαι σίγουρος; Πάρε το πλήρες.** Το lite μόνο για μικρή λήψη ή μικρό stick. Και τα δύο τρέχουν από το stick και κρατούν τα δεδομένα σου πάνω του.

**Μέγεθος stick:** προτείνονται 32 GB ή περισσότερα (τα 64 GB είναι ιδανικά και συνήθως ταχύτερα)· ελάχιστο 8 GB. Κάνε format το stick σε **NTFS** (σε Linux, το exFAT και το FAT32 δεν μπορούν να κρατήσουν τα δεδομένα του browser), αποσυμπίεσε και αντέγραψε ό,τι είναι μέσα στον φάκελο `orOS-Portable` στο stick. Μετά διπλό κλικ στο `orOS.exe` (Windows) ή `./orOS` (Linux).

`orOS-sticktest-*`: προαιρετικό τεστ ταχύτητας. Το αντιγράφεις στο stick και το τρέχεις από εκεί για να δεις αν το stick είναι αρκετά γρήγορο. Αρχεία που τελειώνουν σε `-src` και `SOURCES.txt`: ο πηγαίος κώδικας όλων όσων περιέχει το release.
