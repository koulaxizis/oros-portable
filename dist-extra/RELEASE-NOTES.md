Install [orOS](https://useoros.online) on any Windows or Linux PC, or run it from a USB stick, with or without internet. Free forever, MIT.

## Install on this computer

| | File | |
|---|---|---|
| **Windows 10/11** | `orOS-Setup-<version>.exe` | No administrator rights needed. Adds orOS to the Start menu; remove it from Settings › Apps. |
| **Linux (any)** | `orOS-<version>-x86_64.AppImage` | One file, no install: make it executable and double-click. It adds itself to the app menu. |
| **Debian, Ubuntu, Mint** | `oros_<version>_amd64.deb` | Open it with the software installer, or `sudo apt install ./oros_*.deb`. Best choice on Ubuntu 24.04+. |

The installed orOS uses its own private browser (ungoogled-chromium), works offline, and keeps itself up to date: new orOS versions download in the background and open the next time you start it (Windows setup and AppImage update themselves too; the .deb tells you when a new version is out). Its data stays on this computer (`%LOCALAPPDATA%\orOS` or `~/.local/share/oros`), apart from useoros.online in your browser and from any stick: use Dropbox sync in orOS to carry it over. Windows may say "Windows protected your PC" because the setup is not signed yet: choose "More info" › "Run anyway".

**orOS Studio with your plugins and sound card:** the installers and the full stick edition carry [orOS Bridge](https://github.com/koulaxizis/oros-bridge), which starts with orOS by itself, so Studio can use your VST3 and CLAP plugins and record through your sound card (ASIO® on Windows; JACK, PipeWire, PulseAudio or ALSA on Linux). The pairing code shows up over orOS the first time Studio connects. ASIO is a registered trademark of Steinberg Media Technologies GmbH.

<img src="https://raw.githubusercontent.com/koulaxizis/oros-bridge/main/asio/logo/asio-compatible-bw.png" alt="ASIO Compatible" height="72">

**Tor button:** the installers and the full stick edition have a **Tor** button next to Internet at the top of orOS. The first press downloads the official Tor Browser (about 110 MB) from torproject.org, checks its signature and keeps it in your orOS folder; after that it opens at once in its own window and updates itself. Each orOS user has their own. [Private browsing](https://github.com/koulaxizis/oros-portable/blob/main/PRIVATE-BROWSING.md)

**Local AI for the Assistant:** the installers and the full stick edition can run a free AI on this computer for orOS's Assistant, so it works without internet and what you write never leaves the computer. Open the Assistant and press **Install local AI** (or System › Local AI): orOS downloads Ollama and a model that suits this computer's memory (about 3 to 7 GB in all, once) and the Assistant uses it with no setting. "More choices" offers a smaller or larger model, the graphics card and Remove, which frees the space. On a stick it stays on the stick; it stops when orOS closes.

**Passwords on web pages:** in the installers and the full stick edition the **Internet** window now opens in the same browser as orOS and carries the orOS add-on, so it fills your orOS Passwords logins, cards and addresses on sites and offers to save new logins (nothing is saved until you press Save in Passwords). Sites you were signed in to in the Internet window ask you to sign in once more.

**Start page:** the **Internet** window now opens on the orOS Start page (search, your favourite bookmarks and a Tor button) instead of a blank search page.

**No "Save password?" from the browser:** orOS's browser no longer offers to save passwords, addresses or cards (it popped up over the sync passphrase); orOS Passwords keeps them.

**Tray icon on Windows:** the installed orOS puts its icon in the tray every time you open it (click it to add a task or search, or press Ctrl+Alt+Shift+Space). Before, it appeared only with "Start with the computer" on, so it was gone after a restart or an update.

**Users:** add people in orOS Settings › Users; each has their own orOS, password and Internet window, and there is an optional guest. Encrypting each user's files is optional and only on orOS Live USB.

Also from free package managers: `scoop bucket add oros https://github.com/koulaxizis/oros-portable` and `scoop install oros`, or `winget install koulaxizis.orOS` (once the package is listed there). [Code signing policy](https://github.com/koulaxizis/oros-portable#code-signing-policy)

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

**Check your download:** every file has a `.sha256` beside it with its checksum. Linux: `sha256sum -c orOS-Portable-<version>.zip.sha256` in the download folder. Windows: in PowerShell, `Get-FileHash orOS-Portable-<version>.zip` must show the same code as the `.sha256` file (open it with Notepad).

---

Εγκαθιστάς το [orOS](https://useoros.online) σε οποιονδήποτε υπολογιστή με Windows ή Linux, ή το τρέχεις από USB stick, με ή χωρίς internet. Δωρεάν για πάντα, MIT.

## Εγκατάσταση στον υπολογιστή

| | Αρχείο | |
|---|---|---|
| **Windows 10/11** | `orOS-Setup-<έκδοση>.exe` | Δεν χρειάζεται διαχειριστής. Μπαίνει στο μενού Έναρξη· αφαιρείται από Ρυθμίσεις › Εφαρμογές. |
| **Linux (κάθε διανομή)** | `orOS-<έκδοση>-x86_64.AppImage` | Ένα αρχείο χωρίς εγκατάσταση: το κάνεις εκτελέσιμο και διπλό κλικ. Μπαίνει μόνο του στο μενού εφαρμογών. |
| **Debian, Ubuntu, Mint** | `oros_<έκδοση>_amd64.deb` | Το ανοίγεις με το κέντρο λογισμικού ή `sudo apt install ./oros_*.deb`. Η καλύτερη επιλογή σε Ubuntu 24.04+. |

Το εγκατεστημένο orOS έχει δικό του ιδιωτικό browser (ungoogled-chromium), δουλεύει χωρίς internet και ενημερώνεται μόνο του: οι νέες εκδόσεις του orOS κατεβαίνουν στο παρασκήνιο και ανοίγουν την επόμενη φορά που θα το ξεκινήσεις (ο εγκαταστάτης των Windows και το AppImage αναβαθμίζονται κι αυτά· το .deb σε ειδοποιεί όταν βγει νέα έκδοση). Τα δεδομένα του μένουν σε αυτόν τον υπολογιστή (`%LOCALAPPDATA%\orOS` ή `~/.local/share/oros`), χωριστά από το useoros.online στον browser σου και από κάθε stick: για να τα μεταφέρεις, χρησιμοποίησε το Dropbox sync του orOS. Τα Windows μπορεί να πουν «Τα Windows προστάτευσαν τον υπολογιστή σας», επειδή ο εγκαταστάτης δεν είναι ακόμα υπογεγραμμένος: «Περισσότερες πληροφορίες» › «Εκτέλεση οπωσδήποτε».

**Κουμπί Tor:** οι εγκαταστάτες και η πλήρης έκδοση για stick έχουν κουμπί **Tor** δίπλα στο Internet, στην πάνω μπάρα του orOS. Το πρώτο πάτημα κατεβάζει το επίσημο Tor Browser (περίπου 110 MB) από το torproject.org, ελέγχει την υπογραφή του και το κρατά στον φάκελο του orOS· μετά ανοίγει αμέσως σε δικό του παράθυρο και ενημερώνεται μόνο του. Κάθε χρήστης του orOS έχει το δικό του. [Ιδιωτική περιήγηση](https://github.com/koulaxizis/oros-portable/blob/main/PRIVATE-BROWSING.md#ιδιωτική-περιήγηση-με-το-oros-portable)

**Τοπικό AI για τον Βοηθό:** οι εγκαταστάτες και η πλήρης έκδοση για stick μπορούν να τρέχουν ένα δωρεάν AI σε αυτόν τον υπολογιστή για τον Βοηθό του orOS, ώστε να δουλεύει χωρίς internet και ό,τι γράφεις να μη φεύγει ποτέ από τον υπολογιστή. Άνοιξε τον Βοηθό και πάτα **Εγκατάσταση τοπικού AI** (ή Σύστημα › Τοπικό AI): το orOS κατεβάζει το Ollama και ένα μοντέλο που ταιριάζει στη μνήμη του υπολογιστή (περίπου 3 έως 7 GB συνολικά, μία φορά) και ο Βοηθός το χρησιμοποιεί χωρίς καμία ρύθμιση. Οι "Περισσότερες επιλογές" δίνουν μικρότερο ή μεγαλύτερο μοντέλο, την κάρτα γραφικών και Αφαίρεση, που ελευθερώνει τον χώρο. Σε stick μένει στο stick· σταματά όταν κλείνει το orOS.

**Κωδικοί στις σελίδες:** στους εγκαταστάτες και στην πλήρη έκδοση για stick το παράθυρο **Internet** ανοίγει πια στον ίδιο browser με το orOS και έχει το add-on του orOS, οπότε συμπληρώνει τους κωδικούς, τις κάρτες και τις διευθύνσεις από τους Κωδικούς του orOS και προτείνει να αποθηκεύσεις νέους κωδικούς (τίποτα δεν αποθηκεύεται αν δεν πατήσεις Αποθήκευση στους Κωδικούς). Οι σελίδες όπου ήσουν συνδεδεμένος στο παράθυρο Internet θα ζητήσουν μία φορά ξανά σύνδεση.

**Χρήστες:** προσθέτεις άτομα στις Ρυθμίσεις › Χρήστες του orOS· ο καθένας έχει το δικό του orOS, κωδικό και παράθυρο Internet, και υπάρχει προαιρετικά επισκέπτης. Η κρυπτογράφηση των αρχείων κάθε χρήστη είναι προαιρετική και μόνο στο orOS Live USB.

Και από ελεύθερους διαχειριστές πακέτων: `scoop bucket add oros https://github.com/koulaxizis/oros-portable` και `scoop install oros`, ή `winget install koulaxizis.orOS` (μόλις καταχωριστεί εκεί). [Code signing policy](https://github.com/koulaxizis/oros-portable#code-signing-policy)

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

**Έλεγξε τη λήψη σου:** κάθε αρχείο έχει δίπλα του ένα `.sha256` με το checksum του. Linux: `sha256sum -c orOS-Portable-<έκδοση>.zip.sha256` στον φάκελο λήψεων. Windows: στο PowerShell, το `Get-FileHash orOS-Portable-<έκδοση>.zip` πρέπει να δείχνει τον ίδιο κωδικό με το αρχείο `.sha256` (άνοιξέ το με το Σημειωματάριο).
