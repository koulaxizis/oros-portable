# Private browsing with orOS Portable

**EN** · [ΕΛ](#ιδιωτική-περιήγηση-με-το-oros-portable)

Your internet provider can see which sites you visit. A **VPN browser extension** hides that. orOS does not include or recommend a VPN provider; pick one yourself and use your own account with it.

## Where it goes

- **Full edition and the installed orOS:** in the **Internet** window (Quick settings › Internet). It is the same browser as orOS, so while the extension is connected orOS's own traffic (sync, Mail and the other apps) goes through it too.
- **Lite edition:** in the computer's own browser (for example Brave, Chromium, Edge, Chrome or Firefox), where orOS also runs.

## Steps (full edition and installed orOS)

1. Open **Internet**.
2. Go to the Chrome Web Store (chromewebstore.google.com) and open the page of the VPN provider's extension. Check the publisher name, because copies with similar names exist.
3. Press **Add to Chromium** and confirm with **Add extension**. (The button comes from the built-in "Chromium Web Store" helper, because this privacy-focused Chromium has no store connection of its own. The browser contacts Google only when you install or update something from the store.)
4. Pin it: the puzzle icon at the top right › pin.
5. Sign in or create an account with the provider, then connect.
6. Check it: open a DNS leak test page. It should show the provider's servers, not your internet provider.
7. Updates: the helper's icon shows a number when an extension has a new version; press it and choose the extension to update it.

In the lite edition use your browser's own extension store, steps 2 and 4 to 6.

## Choosing a provider

- A free plan without a data cap, or with one large enough for what you do.
- Its no-logs claim checked by an independent audit, and a privacy policy you can read.
- Open-source apps and extensions are a plus.
- Avoid free VPNs from unknown companies: many sell your browsing data.

## Tor button (full edition and installers)

Next to **Internet** at the top of orOS, the **Tor** button opens the official Tor Browser in its own window. Close it to return to orOS.

- The first press asks, then downloads Tor Browser (about 110 MB) from torproject.org, checks its signature against the Tor Project's signing key and installs it in your own orOS folder (`data/tor` on the stick, `%LOCALAPPDATA%\orOS\tor` or `~/.local/share/oros/tor` when installed). The button shows how far it is. It needs about 1 GB free.
- After that it opens at once and Tor Browser updates itself.
- Each orOS user has their own Tor Browser, with their own bookmarks and settings. The guest has none.
- The System panel (Quick settings › System) has **Remove Tor Browser**, which frees the space again.
- On Windows the official installer runs silently into that folder; the shortcuts and the Add or remove programs entry it makes are taken away again. Windows may still remember that a program ran from the stick (for example in its Prefetch list). For no trace at all on the computer, use orOS USB, which starts without the computer's own system.
- The lite edition has no Tor button: there, install Tor Browser from torproject.org yourself.

## Good to know

- Only the browser with the extension is covered. Streams opened inside orOS (Television, Radio) use your normal connection.
- On Windows, extensions you add cannot read or change orOS's own pages (a Chromium policy that orOS sets keeps every extension except orOS's own away from them), so a VPN extension never sees your unlocked notes or passwords. On Linux this protection needs a system setting a stick cannot make: add only extensions you trust, from publishers you know.
- A browser extension is a proxy, not a full VPN. Before you press Connect, or if the connection drops, the browser uses your normal connection. Check the icon first.
- For really sensitive searches, Tor Browser (the Tor button above) hides more than a VPN, but it is too slow for video. Your internet provider can see that you use Tor.
- A private (incognito) window hides nothing from your internet provider; it only keeps no history on the computer.

---

# Ιδιωτική περιήγηση με το orOS Portable

[EN](#private-browsing-with-oros-portable) · **ΕΛ**

Ο πάροχος internet σου βλέπει ποιες σελίδες επισκέπτεσαι. Ένα **VPN extension για τον browser** το κρύβει. Το orOS δεν περιέχει ούτε προτείνει πάροχο VPN· διαλέγεις εσύ και χρησιμοποιείς τον δικό σου λογαριασμό.

## Πού μπαίνει

- **Πλήρης έκδοση και εγκατεστημένο orOS:** στο παράθυρο **Internet** (Γρήγορες ρυθμίσεις › Internet). Είναι ο ίδιος browser με το orOS, οπότε όσο το extension είναι συνδεδεμένο περνά από αυτό και η κίνηση του ίδιου του orOS (sync, Mail και οι άλλες εφαρμογές).
- **Έκδοση Lite:** στον κανονικό browser του υπολογιστή (π.χ. Brave, Chromium, Edge, Chrome ή Firefox), όπου τρέχει και το orOS.

## Βήματα (πλήρης έκδοση και εγκατεστημένο orOS)

1. Άνοιξε το **Internet**.
2. Πήγαινε στο Chrome Web Store (chromewebstore.google.com) και άνοιξε τη σελίδα του extension του παρόχου VPN. Έλεγξε το όνομα του εκδότη, γιατί υπάρχουν αντίγραφα με παρόμοια ονόματα.
3. Πάτα **Add to Chromium** και επιβεβαίωσε με **Add extension**. (Το κουμπί το βάζει ο ενσωματωμένος βοηθός «Chromium Web Store», γιατί αυτός ο Chromium, φτιαγμένος για ιδιωτικότητα, δεν έχει δική του σύνδεση με το κατάστημα. Ο browser μιλά με τη Google μόνο όταν εγκαθιστάς ή ενημερώνεις κάτι από το κατάστημα.)
4. Καρφίτσωσέ το: το εικονίδιο με το κομμάτι παζλ πάνω δεξιά › καρφίτσωμα.
5. Συνδέσου ή φτιάξε λογαριασμό στον πάροχο και πάτα σύνδεση.
6. Έλεγξέ το: άνοιξε μια σελίδα DNS leak test. Πρέπει να δείχνει τους servers του παρόχου, όχι τον πάροχο internet σου.
7. Ενημερώσεις: το εικονίδιο του βοηθού δείχνει αριθμό όταν ένα extension έχει νέα έκδοση· πάτα το και διάλεξε το extension για να ενημερωθεί.

Στην έκδοση Lite χρησιμοποίησε το κατάστημα extensions του browser σου, βήματα 2 και 4 έως 6.

## Πώς διαλέγεις πάροχο

- Δωρεάν πρόγραμμα χωρίς όριο δεδομένων, ή με όριο που σου φτάνει.
- Η δήλωση «no logs» ελεγμένη από ανεξάρτητο audit, και πολιτική απορρήτου που διαβάζεται.
- Ανοιχτός κώδικας στις εφαρμογές και στο extension είναι συν.
- Απόφυγε δωρεάν VPN άγνωστων εταιρειών: πολλά πουλάνε τα δεδομένα περιήγησης.

## Κουμπί Tor (πλήρης έκδοση και εγκαταστάτες)

Δίπλα στο **Internet**, στην πάνω μπάρα του orOS, το κουμπί **Tor** ανοίγει το επίσημο Tor Browser σε δικό του παράθυρο. Κλείσ' το για να γυρίσεις στο orOS.

- Το πρώτο πάτημα ρωτά και μετά κατεβάζει το Tor Browser (περίπου 110 MB) από το torproject.org, ελέγχει την υπογραφή του με το κλειδί του Tor Project και το εγκαθιστά στον δικό σου φάκελο του orOS (`data/tor` στο stick, `%LOCALAPPDATA%\orOS\tor` ή `~/.local/share/oros/tor` στην εγκατάσταση). Το κουμπί δείχνει την πρόοδο. Χρειάζεται περίπου 1 GB ελεύθερο.
- Από εκεί και πέρα ανοίγει αμέσως, και το Tor Browser ενημερώνεται μόνο του.
- Κάθε χρήστης του orOS έχει δικό του Tor Browser, με δικούς του σελιδοδείκτες και ρυθμίσεις. Ο επισκέπτης δεν έχει.
- Ο πίνακας System (Γρήγορες ρυθμίσεις › System) έχει το **Αφαίρεση του Tor Browser**, που ελευθερώνει ξανά τον χώρο.
- Στα Windows ο επίσημος installer τρέχει σιωπηλά σε αυτόν τον φάκελο· τα shortcuts και η εγγραφή στην «Προσθαφαίρεση προγραμμάτων» που φτιάχνει αφαιρούνται ξανά. Τα Windows μπορεί πάλι να θυμούνται ότι έτρεξε ένα πρόγραμμα από το stick (π.χ. στη λίστα Prefetch). Για κανένα ίχνος στον υπολογιστή, χρησιμοποίησε το orOS USB, που ξεκινά χωρίς το σύστημα του υπολογιστή.
- Η έκδοση Lite δεν έχει κουμπί Tor: εκεί εγκατάστησε μόνος σου το Tor Browser από το torproject.org.

## Καλό να ξέρεις

- Καλύπτεται μόνο ο browser με το extension. Ό,τι ανοίγει μέσα στο orOS (Television, Radio) περνά από την κανονική σου σύνδεση.
- Στα Windows, τα extensions που προσθέτεις δεν μπορούν να διαβάσουν ή να αλλάξουν τις σελίδες του ίδιου του orOS (μια πολιτική του Chromium που βάζει το orOS κρατά μακριά τους όλα τα extensions εκτός από τα δικά του orOS), οπότε ένα VPN extension δεν βλέπει ποτέ τις ξεκλείδωτες σημειώσεις ή τους κωδικούς σου. Στο Linux αυτή η προστασία θέλει ρύθμιση συστήματος που ένα stick δεν μπορεί να κάνει: βάλε μόνο extensions που εμπιστεύεσαι, από εκδότες που ξέρεις.
- Ένα extension είναι proxy, όχι πλήρες VPN. Πριν πατήσεις σύνδεση, ή αν πέσει η σύνδεση, ο browser βγαίνει από την κανονική σου σύνδεση. Κοίτα πρώτα το εικονίδιο.
- Για πραγματικά ευαίσθητες αναζητήσεις, το Tor Browser (το κουμπί Tor πιο πάνω) κρύβει περισσότερα από ένα VPN, αλλά είναι πολύ αργό για βίντεο. Ο πάροχος internet βλέπει ότι χρησιμοποιείς Tor.
- Ένα ιδιωτικό (incognito) παράθυρο δεν κρύβει τίποτα από τον πάροχο internet· απλώς δεν κρατά ιστορικό στον υπολογιστή.
