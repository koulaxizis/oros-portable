// orOS sign-in window (launcher/usersapi.go). It runs in its own empty
// browser profile (data/login), so it never sees anybody's orOS data.
// Text goes in only through textContent. After a sign-in the launcher
// closes this window and opens the user's own orOS.
(() => {
  "use strict";

  const TEXT = {
    en: {
      who: "Who is using orOS?", password: "Password or PIN", show: "Show", hide: "Hide", signIn: "Sign in",
      back: "Other user", forgot: "Forgot your password?", guest: "Guest", guestNote: "Nothing is kept",
      owner: "Owner", wrong: "Wrong password.", wait: "Too many tries. Try again in {s} s.",
      busy: "Someone else is using orOS on this computer right now.", error: "Something went wrong. Try again.",
      recTitle: "Set a new password", recHelp: "Type the recovery code you wrote down when the owner's password was set. It sets a new password; your data stays as it is.",
      recCode: "Recovery code", recPw: "New password or PIN", recPw2: "The same again", recGo: "Set the new password",
      recBack: "Back", recWrong: "This recovery code is not right.", pwShort: "Use a PIN of 4 to 12 digits or at least 6 characters.",
      pwDiffer: "The two passwords differ.", codeTitle: "Your new recovery code",
      codeHelp: "Write it down and keep it somewhere safe. It is the only way back in if the owner's password is forgotten. The old code no longer works.",
      codeOk: "I wrote it down", opening: "Opening orOS…", close: "Close", none: "There are no users here yet.",
      recHelpVault: "Your files are encrypted. Type the recovery code you got when you turned encryption on. It sets a new password; your files stay as they are.",
      codeHelpVault: "Write it down and keep it somewhere safe. Your files are encrypted: without your password or this code nobody can open them, not even the owner. The old code no longer works.",
      sealing: "Encrypting your files…", unsealing: "Opening your encrypted files…", unsealingOff: "Turning encryption off…",
      longNote: "This can take a few minutes. Keep the computer on.", vaultFail: "Your encrypted files could not be opened.",
    },
    el: {
      who: "Ποιος χρησιμοποιεί το orOS;", password: "Κωδικός ή PIN", show: "Εμφάνιση", hide: "Απόκρυψη", signIn: "Είσοδος",
      back: "Άλλος χρήστης", forgot: "Ξεχάσατε τον κωδικό;", guest: "Επισκέπτης", guestNote: "Τίποτα δεν κρατιέται",
      owner: "Κάτοχος", wrong: "Λάθος κωδικός.", wait: "Πολλές προσπάθειες. Δοκιμάστε ξανά σε {s} δ.",
      busy: "Κάποιος άλλος χρησιμοποιεί αυτή τη στιγμή το orOS σε αυτόν τον υπολογιστή.", error: "Κάτι πήγε στραβά. Δοκιμάστε ξανά.",
      recTitle: "Νέος κωδικός", recHelp: "Γράψτε τον κωδικό ανάκτησης που σημειώσατε όταν ορίστηκε ο κωδικός του κατόχου. Με αυτόν ορίζεται νέος κωδικός· τα δεδομένα σας μένουν όπως είναι.",
      recCode: "Κωδικός ανάκτησης", recPw: "Νέος κωδικός ή PIN", recPw2: "Ο ίδιος ξανά", recGo: "Ορισμός νέου κωδικού",
      recBack: "Πίσω", recWrong: "Αυτός ο κωδικός ανάκτησης δεν είναι σωστός.", pwShort: "Χρησιμοποιήστε PIN 4 έως 12 ψηφίων ή τουλάχιστον 6 χαρακτήρες.",
      pwDiffer: "Οι δύο κωδικοί διαφέρουν.", codeTitle: "Ο νέος κωδικός ανάκτησης",
      codeHelp: "Σημειώστε τον και φυλάξτε τον κάπου ασφαλές. Είναι ο μόνος τρόπος να μπείτε αν ξεχαστεί ο κωδικός του κατόχου. Ο παλιός κωδικός δεν ισχύει πια.",
      codeOk: "Τον σημείωσα", opening: "Άνοιγμα του orOS…", close: "Κλείσιμο", none: "Δεν υπάρχουν ακόμα χρήστες εδώ.",
      recHelpVault: "Τα αρχεία σας είναι κρυπτογραφημένα. Γράψτε τον κωδικό ανάκτησης που πήρατε όταν ενεργοποιήσατε την κρυπτογράφηση. Με αυτόν ορίζεται νέος κωδικός· τα αρχεία σας μένουν όπως είναι.",
      codeHelpVault: "Σημειώστε τον και φυλάξτε τον κάπου ασφαλές. Τα αρχεία σας είναι κρυπτογραφημένα: χωρίς τον κωδικό σας ή αυτόν τον κωδικό δεν τα ανοίγει κανείς, ούτε ο κάτοχος. Ο παλιός κωδικός δεν ισχύει πια.",
      sealing: "Κρυπτογράφηση των αρχείων σας…", unsealing: "Άνοιγμα των κρυπτογραφημένων αρχείων σας…", unsealingOff: "Απενεργοποίηση της κρυπτογράφησης…",
      longNote: "Μπορεί να πάρει μερικά λεπτά. Μην κλείσετε τον υπολογιστή.", vaultFail: "Δεν ήταν δυνατό να ανοίξουν τα κρυπτογραφημένα αρχεία σας.",
    },
    de: {
      who: "Wer benutzt orOS?", password: "Passwort oder PIN", show: "Anzeigen", hide: "Verbergen", signIn: "Anmelden",
      back: "Anderer Benutzer", forgot: "Passwort vergessen?", guest: "Gast", guestNote: "Nichts wird behalten",
      owner: "Besitzer", wrong: "Falsches Passwort.", wait: "Zu viele Versuche. Versuche es in {s} s erneut.",
      busy: "Gerade benutzt jemand anderes orOS auf diesem Computer.", error: "Etwas ist schiefgegangen. Versuche es erneut.",
      recTitle: "Neues Passwort festlegen", recHelp: "Gib den Wiederherstellungscode ein, den du dir notiert hast, als das Passwort des Besitzers festgelegt wurde. Er setzt ein neues Passwort; deine Daten bleiben, wie sie sind.",
      recCode: "Wiederherstellungscode", recPw: "Neues Passwort oder PIN", recPw2: "Noch einmal", recGo: "Neues Passwort festlegen",
      recBack: "Zurück", recWrong: "Dieser Wiederherstellungscode stimmt nicht.", pwShort: "Verwende eine PIN mit 4 bis 12 Ziffern oder mindestens 6 Zeichen.",
      pwDiffer: "Die beiden Passwörter sind verschieden.", codeTitle: "Dein neuer Wiederherstellungscode",
      codeHelp: "Schreib ihn auf und bewahre ihn sicher auf. Nur mit ihm kommst du wieder hinein, wenn das Passwort des Besitzers vergessen ist. Der alte Code gilt nicht mehr.",
      codeOk: "Ich habe ihn notiert", opening: "orOS wird geöffnet…", close: "Schließen", none: "Hier gibt es noch keine Benutzer.",
      recHelpVault: "Deine Dateien sind verschlüsselt. Gib den Wiederherstellungscode ein, den du beim Einschalten der Verschlüsselung bekommen hast. Er setzt ein neues Passwort; deine Dateien bleiben, wie sie sind.",
      codeHelpVault: "Schreib ihn auf und bewahre ihn sicher auf. Deine Dateien sind verschlüsselt: Ohne dein Passwort oder diesen Code kann sie niemand öffnen, auch nicht der Besitzer. Der alte Code gilt nicht mehr.",
      sealing: "Deine Dateien werden verschlüsselt…", unsealing: "Deine verschlüsselten Dateien werden geöffnet…", unsealingOff: "Verschlüsselung wird ausgeschaltet…",
      longNote: "Das kann einige Minuten dauern. Lass den Computer eingeschaltet.", vaultFail: "Deine verschlüsselten Dateien konnten nicht geöffnet werden.",
    },
  };

  const $ = (id) => document.getElementById(id);
  let lang = "en";
  let state = null;
  let chosen = null;
  let waitTimer = 0;

  const t = (k, vars) => {
    let s = (TEXT[lang] && TEXT[lang][k]) || TEXT.en[k] || k;
    for (const [n, v] of Object.entries(vars || {})) s = s.replace("{" + n + "}", String(v));
    return s;
  };

  function store(k, v) {
    try { if (v == null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch (e) { /* private profile */ }
  }
  function load(k) {
    try { return localStorage.getItem(k); } catch (e) { return null; }
  }

  // ---------- API ----------
  async function api(name, body) {
    const opt = body === undefined
      ? { method: "GET", credentials: "same-origin" }
      : { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json", "X-Oros-Users": "1" }, body: JSON.stringify(body) };
    try {
      const r = await fetch("/__oros/users/" + name, opt);
      const j = await r.json().catch(() => ({}));
      return j && typeof j === "object" ? j : {};
    } catch (e) {
      return { error: "network" };
    }
  }

  // ---------- Drawing ----------
  function initial(name) {
    const s = String(name || "").trim();
    if (!s) return "?";
    return Array.from(s)[0].toLocaleUpperCase(lang);
  }

  function show(id) {
    for (const s of ["pick", "pass", "rec", "code", "opening"]) $(s).hidden = s !== id;
    const f = { pass: "pw", rec: "rec-code", code: "code-ok" }[id];
    if (f) setTimeout(() => $(f).focus(), 30);
  }

  function setText() {
    document.documentElement.lang = lang;
    const map = {
      "t-who": "who", "t-pw": "password", "pass-go": "signIn", "pass-back": "back", "pass-forgot": "forgot",
      "t-rec": "recTitle", "t-rec-help": "recHelp", "t-rec-code": "recCode", "t-rec-pw": "recPw", "t-rec-pw2": "recPw2",
      "rec-go": "recGo", "rec-back": "recBack", "t-code": "codeTitle", "t-code-help": "codeHelp", "code-ok": "codeOk",
      "t-opening": "opening", "close": "close",
    };
    for (const [id, k] of Object.entries(map)) $(id).textContent = t(k);
    $("pw").placeholder = t("password");
    $("pw-show").textContent = $("pw").type === "password" ? t("show") : t("hide");
    for (const b of document.querySelectorAll("[data-lang]")) b.setAttribute("aria-pressed", String(b.dataset.lang === lang));
    tick();
    if (state) drawUsers();
  }

  function tick() {
    const d = new Date();
    const loc = { en: "en-GB", el: "el-GR", de: "de-DE" }[lang] || "en-GB";
    $("clock").textContent = d.toLocaleTimeString(loc, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
    $("date").textContent = d.toLocaleDateString(loc, { weekday: "long", day: "numeric", month: "long" });
  }

  function tile(u) {
    const li = document.createElement("li");
    const b = document.createElement("button");
    b.type = "button";
    const av = document.createElement("span");
    av.className = "avatar" + (u.guest ? " guest" : "");
    av.setAttribute("aria-hidden", "true");
    av.textContent = u.guest ? "?" : initial(u.name);
    const n = document.createElement("span");
    n.className = "name";
    n.textContent = u.guest ? t("guest") : u.name;
    b.append(av, n);
    if (u.guest || u.owner) {
      const note = document.createElement("span");
      note.className = "note";
      note.textContent = u.guest ? t("guestNote") : t("owner");
      b.append(note);
    }
    b.addEventListener("click", () => (u.guest ? signInGuest() : choose(u)));
    li.append(b);
    return li;
  }

  function drawUsers() {
    const ul = $("users");
    ul.replaceChildren();
    const list = (state.users || []).filter((u) => u.password);
    for (const u of list) ul.append(tile(u));
    if (state.guest) ul.append(tile({ guest: true }));
    if (!ul.children.length) {
      const li = document.createElement("li");
      li.textContent = t("none");
      ul.append(li);
    }
  }

  function choose(u) {
    chosen = u;
    $("pass-avatar").textContent = initial(u.name);
    $("pass-name").textContent = u.name;
    $("pw").value = "";
    $("pw").type = "password";
    $("pw-show").textContent = t("show");
    $("pass-forgot").hidden = !((u.owner && state.recovery) || isVault(u));
    message("pass-msg", "");
    show("pass");
    if (u.wait) waitMessage("pass-msg", u.wait);
  }

  function message(id, s) { $(id).textContent = s; }

  // An encrypted user: only their password or their own recovery code
  // opens their files (launcher/vault.go).
  const isVault = (u) => !!u && (u.vault === "on" || u.vault === "off-pending");

  function waitMessage(id, ms) {
    clearInterval(waitTimer);
    const end = Date.now() + ms;
    const go = $(id === "pass-msg" ? "pass-go" : "rec-go");
    const step = () => {
      const left = Math.ceil((end - Date.now()) / 1000);
      if (left <= 0) { clearInterval(waitTimer); message(id, ""); go.disabled = false; return; }
      go.disabled = true;
      message(id, t("wait", { s: left }));
    };
    step();
    waitTimer = setInterval(step, 1000);
  }

  function answerError(id, r) {
    if (r.wait) return waitMessage(id, r.wait);
    const k = { wrong: id === "rec-msg" ? "recWrong" : "wrong", busy: "busy", password: "pwShort", vault: "vaultFail" }[r.error] || "error";
    message(id, t(k));
  }

  // ---------- Sign in ----------
  function opened() {
    $("t-opening").textContent = t("opening");
    $("opening-note").textContent = "";
    show("opening");
    // The launcher closes this window once it sees the sign-in; closing
    // it here as well is quicker where the browser allows it.
    setTimeout(() => { try { window.close(); } catch (e) { /* the launcher closes it */ } }, 600);
  }

  async function signInGuest() {
    const r = await api("login", { id: "guest" });
    if (r.ok) return opened();
    show("pick");
    alertTop(r);
  }

  function alertTop(r) {
    const k = r.error === "busy" ? "busy" : "error";
    $("t-who").textContent = t(k);
    setTimeout(() => { $("t-who").textContent = t("who"); }, 5000);
  }

  $("pass-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    if (!chosen || $("pass-go").disabled) return;
    $("pass-go").disabled = true;
    message("pass-msg", "");
    // Encrypting or opening encrypted files takes a moment (a few minutes
    // the first time): say so while the sign-in waits.
    const v = chosen.vault;
    const slow = setTimeout(() => {
      const k = v === "pending" ? "sealing" : v === "off-pending" ? "unsealingOff" : isVault(chosen) ? "unsealing" : "opening";
      $("t-opening").textContent = t(k);
      $("opening-note").textContent = k === "sealing" || k === "unsealingOff" ? t("longNote") : "";
      show("opening");
    }, 700);
    const r = await api("login", { id: chosen.id, password: $("pw").value });
    clearTimeout(slow);
    $("pass-go").disabled = false;
    if (r.ok) return opened();
    show("pass");
    $("pw").select();
    answerError("pass-msg", r);
  });

  $("pw-show").addEventListener("click", () => {
    const pw = $("pw");
    pw.type = pw.type === "password" ? "text" : "password";
    $("pw-show").textContent = pw.type === "password" ? t("show") : t("hide");
    pw.focus();
  });
  $("pass-back").addEventListener("click", () => { clearInterval(waitTimer); show("pick"); refresh(); });
  $("pass-forgot").addEventListener("click", () => {
    for (const id of ["rec-code", "rec-pw", "rec-pw2"]) $(id).value = "";
    message("rec-msg", "");
    $("t-rec-help").textContent = t(isVault(chosen) ? "recHelpVault" : "recHelp");
    show("rec");
  });

  // ---------- Recovery ----------
  const validPassword = (p) => /^[0-9]{4,12}$/.test(p) || (Array.from(p).length >= 6 && Array.from(p).length <= 256);

  $("rec-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    if ($("rec-go").disabled) return;
    const pw = $("rec-pw").value;
    if (!validPassword(pw)) return message("rec-msg", t("pwShort"));
    if (pw !== $("rec-pw2").value) return message("rec-msg", t("pwDiffer"));
    $("rec-go").disabled = true;
    const r = await api("recover", { id: chosen ? chosen.id : "", code: $("rec-code").value, password: pw });
    $("rec-go").disabled = false;
    if (!r.ok) return answerError("rec-msg", r);
    $("t-code-help").textContent = t(isVault(chosen) ? "codeHelpVault" : "codeHelp");
    $("code-value").textContent = String(r.code || "");
    show("code");
  });
  $("rec-back").addEventListener("click", () => show("pass"));
  $("code-ok").addEventListener("click", () => { $("pw").value = ""; message("pass-msg", ""); show("pass"); });

  // ---------- Footer ----------
  for (const b of document.querySelectorAll("[data-lang]")) {
    b.addEventListener("click", () => { lang = b.dataset.lang; store("oros-login-lang", lang); setText(); });
  }
  $("close").addEventListener("click", () => window.close());

  async function refresh() {
    const r = await api("state");
    if (r.error) return;
    state = r;
    drawUsers();
  }

  async function start() {
    const saved = load("oros-login-lang");
    const r = await api("state");
    state = r.error ? { users: [] } : r;
    lang = TEXT[saved] ? saved : (TEXT[state.lang] ? state.lang : "en");
    // orOS USB: closing the window would only bring it back; the System
    // extension adds Restart and Shut down there.
    $("close").hidden = !!state.kiosk;
    setText();
    show("pick");
    const first = document.querySelector("#users button");
    if (first) first.focus();
    setInterval(tick, 15000);
  }

  start();
})();
