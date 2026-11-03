package main

// Local AI for orOS's Assistant (System extension › Local AI), as on orOS
// USB (oros-usb system/ai.go): one button downloads the official Ollama
// build from its GitHub release, checked against the SHA-256 that this
// build pinned (scripts/build.sh writes ollama-<os>.json next to the
// launcher), keeps only what runs on the processor and, through Vulkan,
// on the graphics card (NVIDIA's CUDA and AMD's ROCm libraries are most of
// the download and are skipped), and then downloads one open model
// (Qwen3, Apache-2.0) through Ollama itself.
//
// One copy serves everyone on this stick or computer: Ollama and its
// models sit in <data>/ai (the stick's data folder, or %LOCALAPPDATA%\orOS
// and ~/.local/share/oros when installed). The models take gigabytes, so
// only the owner adds or removes them; everyone may use them.
//
// Ollama listens on 127.0.0.1:11434 only and lets in only the orOS page
// (OLLAMA_ORIGINS). The launcher that serves orOS starts it with the
// server and stops it with the server (a stick must be free to unplug);
// an Ollama the person installed themselves is used as it is and never
// stopped.

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	aiAddr   = "127.0.0.1:11434"
	aiFolder = "ai"
	// The downloads hold the graphics libraries too (1.5 to 1.7 GB); what
	// is kept is about 120 MB.
	aiMaxDownload = 4 << 30
	aiMaxKept     = 400 << 20
	// aiFAT32Max: FAT32 cannot hold a file of 4 GiB.
	aiFAT32Max = 4000
)

var (
	aiAPI   = "http://" + aiAddr
	aiHTTP  = &http.Client{Timeout: 2 * time.Hour}
	aiQuick = &http.Client{Timeout: 2 * time.Second}
	// aiURLOK checks the pinned address; tests replace it.
	aiURLOK = aiURL
	// aiGOOS picks the download's layout; tests replace it.
	aiGOOS = runtime.GOOS
	// aiOrigin is the orOS page, the only one Ollama lets in.
	aiOrigin = strings.TrimSuffix(appURL, "/")
)

// aiModels are the models offered, smallest first, with their download
// size in MB (ollama.com/library/qwen3). qwen3:0.6b is for the CI test
// only and is not shown.
var aiModels = []struct {
	Name string
	MB   int
	Show bool
}{
	{"qwen3:0.6b", 523, false},
	{"qwen3:1.7b", 1400, true},
	{"qwen3:4b", 2500, true},
	{"qwen3:8b", 5200, true},
}

func aiModelMB(name string) (int, bool) {
	for _, m := range aiModels {
		if m.Name == name {
			return m.MB, true
		}
	}
	return 0, false
}

// aiRecommend picks the model for this much memory: the 8b needs about
// 6 GB free while it answers, the 4b about 3.5 GB. A "16 GB" computer
// reports a little less than 16 GiB. maxMB leaves out models that do not
// fit the folder's file system.
func aiRecommend(memGiB float64, maxMB int) string {
	want := "qwen3:1.7b"
	switch {
	case memGiB >= 14:
		want = "qwen3:8b"
	case memGiB >= 7:
		want = "qwen3:4b"
	}
	best := ""
	for _, m := range aiModels {
		if m.Show && (maxMB <= 0 || m.MB < maxMB) {
			best = m.Name
			if m.Name == want {
				break
			}
		}
	}
	return best
}

func aiDir(lay layout) string          { return filepath.Join(lay.data, aiFolder) }
func aiProgressFile(dir string) string { return filepath.Join(dir, "progress.json") }
func aiModelsDir(dir string) string    { return filepath.Join(dir, "models") }
func aiPinFile(lay layout) string {
	return filepath.Join(lay.root, "ollama-"+runtime.GOOS+".json")
}

// aiBinIn is Ollama's program inside an unpacked copy: bin/ollama in the
// Linux archive, ollama.exe at the top of the Windows one.
func aiBinIn(dir string) string {
	if aiGOOS == "windows" {
		return filepath.Join(dir, "ollama", "ollama.exe")
	}
	return filepath.Join(dir, "ollama", "bin", "ollama")
}

func aiInstalled(dir string) bool { _, err := os.Stat(aiBinIn(dir)); return err == nil }

// aiGPU is false once the person turned the graphics card off (a card
// whose driver misbehaves); Ollama then uses the processor only.
func aiGPU(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "cpu-only"))
	return err != nil
}

// aiSupported: Ollama's builds for 64-bit Windows and Linux.
func aiSupported() bool {
	return runtime.GOARCH == "amd64" && (runtime.GOOS == "windows" || runtime.GOOS == "linux") &&
		os.Getenv("OROS_NO_AI") == ""
}

// aiAvailable: the button is offered where the extension runs and this
// build pinned a download (the full edition and the installers), or
// Ollama is here already.
func aiAvailable(lay layout) bool {
	if !aiSupported() {
		return false
	}
	if _, err := readAIPin(aiPinFile(lay)); err == nil {
		return true
	}
	return aiInstalled(aiDir(lay))
}

// aiMayManage: without users everyone; with users only the owner adds or
// removes models (they are shared and take gigabytes), never the guest.
func aiMayManage(lay layout) bool {
	l := loadUsers(lay.data)
	if !l.enabled() {
		return true
	}
	o := l.owner()
	return o != nil && filepath.Clean(userDir(lay.data, o)) == filepath.Clean(lay.userFolder())
}

func aiBusy(dir string) bool { return held(filepath.Join(dir, ".lock")) }

func aiLock(dir string) (func(), error) {
	unlock, ok := lockFile(filepath.Join(dir, ".lock"))
	if !ok {
		return nil, errors.New("local AI is already being set up")
	}
	return unlock, nil
}

// aiAnswers is true when an Ollama answers on its address and lets the
// orOS page in (an Ollama the person runs may refuse it).
func aiAnswers() (up, allowed bool) {
	req, _ := http.NewRequest(http.MethodGet, aiAPI+"/api/version", nil)
	req.Header.Set("Origin", aiOrigin)
	resp, err := aiQuick.Do(req)
	if err != nil {
		return false, false
	}
	resp.Body.Close()
	return true, resp.StatusCode == http.StatusOK
}

func aiRunning() bool { _, ok := aiAnswers(); return ok }

// aiList is the models Ollama has, by name.
func aiList() ([]string, error) {
	resp, err := aiQuick.Get(aiAPI + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var t struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return nil, err
	}
	out := []string{}
	for _, m := range t.Models {
		out = append(out, m.Name)
	}
	return out, nil
}

var errAIForeign = errors.New("another Ollama runs on this computer and does not let orOS in: add " +
	"http://localhost:47800 to its OLLAMA_ORIGINS, or close it")

// aiServe starts Ollama in the background when it is installed and no
// Ollama answers yet. It outlives the caller, like the browser.
func aiServe(dir string) error {
	if up, allowed := aiAnswers(); up {
		if !allowed {
			return errAIForeign
		}
		return nil
	}
	if !aiInstalled(dir) {
		return nil
	}
	cmd := exec.Command(aiBinIn(dir), "serve")
	cmd.Env = append(os.Environ(),
		"OLLAMA_HOST="+aiAddr,
		"OLLAMA_ORIGINS="+aiOrigin+",http://127.0.0.1:47800",
		"OLLAMA_MODELS="+aiModelsDir(dir),
		// One answer at a time and one model in memory: the computer stays usable.
		"OLLAMA_NUM_PARALLEL=1",
		"OLLAMA_MAX_LOADED_MODELS=1",
		// Only the models on this computer, never Ollama's cloud models.
		"OLLAMA_NO_CLOUD=1",
		// Its key and caches stay in this folder too, not on the computer
		// a stick is plugged into.
		"HOME="+dir,
		"USERPROFILE="+dir,
	)
	if !aiGPU(dir) {
		cmd.Env = append(cmd.Env, "OLLAMA_VULKAN=0")
	}
	if f, err := os.OpenFile(filepath.Join(dir, "serve.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := aiSpawn(cmd); err != nil {
		return err
	}
	for i := 0; i < 60; i++ {
		if aiRunning() {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("the local AI did not start (see ai/serve.log)")
}

// aiOurs: the Ollama on the port is this folder's (not one the person
// installed), so orOS may stop it.
func aiOurs(dir string) (portOwner, bool) {
	o, found := findPortOwner(11434)
	if !found || o.exe == "" {
		return o, false
	}
	exe := strings.TrimSuffix(o.exe, " (deleted)")
	bin := aiBinIn(dir)
	if runtime.GOOS == "windows" {
		return o, strings.EqualFold(filepath.Clean(exe), filepath.Clean(bin))
	}
	return o, filepath.Clean(exe) == filepath.Clean(bin)
}

// aiStop ends this folder's Ollama with its model runners.
func aiStop(dir string) {
	o, ours := aiOurs(dir)
	if !ours {
		return
	}
	if p, err := os.FindProcess(o.pid); err == nil {
		killTree(p)
	}
	for i := 0; i < 20 && aiRunning(); i++ {
		time.Sleep(250 * time.Millisecond)
	}
}

// startAI and stopAI go with the orOS server (main.go, tray.go). A setup
// still downloading keeps its Ollama until it is done.
func startAI(lay layout) {
	dir := aiDir(lay)
	if !aiSupported() || !aiInstalled(dir) {
		return
	}
	go func() {
		if err := aiServe(dir); err != nil {
			log.Printf("local AI: %v", err)
		}
	}()
}

func stopAI(lay layout) {
	dir := aiDir(lay)
	if !aiSupported() || !aiInstalled(dir) || aiBusy(dir) {
		return
	}
	aiStop(dir)
}

func aiFreeMB(dir string) int64 {
	free, _, _ := diskSpace(dir)
	if free <= 0 {
		return -1
	}
	return free >> 20
}

// aiMaxMB is the largest model file the folder's file system holds (0:
// no limit).
func aiMaxMB(dir string) int {
	if aiFAT(dir) {
		return aiFAT32Max
	}
	return 0
}

// --- the native host's commands ---

type aiState struct {
	Step    string `json:"step"`
	Percent int    `json:"percent"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

func aiWriter(file string) func(aiState) {
	last := aiState{Percent: -1}
	return func(p aiState) {
		if p.Step == last.Step && p.Percent == last.Percent && !p.Done {
			return
		}
		last = p
		b, _ := json.Marshal(p)
		_ = os.MkdirAll(filepath.Dir(file), 0o700)
		tmp := file + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, file)
		}
	}
}

// aiStatus is "ai.status": the same fields as on orOS USB.
func aiStatus(lay layout) sysReply {
	if !aiSupported() {
		return sysFail(errUnsupported)
	}
	dir := aiDir(lay)
	_ = os.MkdirAll(dir, 0o700)
	mem := memGiB()
	maxMB := aiMaxMB(dir)
	r := sysReply{
		"running":     aiBusy(dir),
		"installed":   aiInstalled(dir),
		"persistent":  true, // the stick or the disk keeps the folder
		"memGiB":      mem,
		"recommended": aiRecommend(mem, maxMB),
		"freeMB":      aiFreeMB(dir),
		"address":     aiAPI,
		"gpu":         aiGPU(dir),
		"available":   aiAvailable(lay),
		// The engine's download, once (the extension's size hint).
		"engineMB": map[bool]int{true: 1700, false: 1500}[runtime.GOOS == "windows"],
	}
	if b, err := os.ReadFile(aiProgressFile(dir)); err == nil {
		var p aiState
		if json.Unmarshal(b, &p) == nil {
			r["progress"] = p
		}
	}
	offer := []sysReply{}
	for _, m := range aiModels {
		if m.Show && (maxMB <= 0 || m.MB < maxMB) {
			offer = append(offer, sysReply{"name": m.Name, "mb": m.MB})
		}
	}
	r["offer"] = offer
	if up, allowed := aiAnswers(); up && allowed {
		r["serving"] = true
		// An Ollama the person runs counts as installed: orOS uses it.
		r["installed"] = true
		if l, err := aiList(); err == nil {
			r["models"] = l
		}
	} else if up {
		r["foreign"] = true
	}
	return ok(r)
}

// aiSelf runs the launcher detached; tests replace it.
var aiSelf = func(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return detach(exe, args...)
}

// aiInstall is "ai.install": the engine (when missing) and the model, in
// a launcher of its own ("orOS ai --fetch"); ai.status follows it.
func aiInstall(lay layout, model string) sysReply {
	if !aiSupported() {
		return sysFail(errUnsupported)
	}
	mb, known := aiModelMB(model)
	if !known {
		return sysFail(errors.New("unknown model"))
	}
	dir := aiDir(lay)
	if max := aiMaxMB(dir); max > 0 && mb >= max {
		return sysFail(errors.New("this model does not fit a FAT32 stick; pick a smaller one"))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return sysFail(err)
	}
	if aiBusy(dir) {
		return ok(sysReply{"downloading": true})
	}
	_ = os.Remove(aiProgressFile(dir))
	aiWriter(aiProgressFile(dir))(aiState{Step: "start"})
	if err := aiSelf("ai", "--fetch", "--model", model, "--dir", dir, "--pin", aiPinFile(lay)); err != nil {
		return sysFail(err)
	}
	for i := 0; i < 20 && !aiBusy(dir); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	return ok(sysReply{"downloading": true})
}

// aiStart is "ai.start": Ollama again after it was stopped or crashed.
func aiStart(lay layout) sysReply {
	if !aiSupported() {
		return sysFail(errUnsupported)
	}
	if err := aiServe(aiDir(lay)); err != nil {
		return sysFail(err)
	}
	return ok(nil)
}

// aiSetGPU is "ai.gpu": the graphics card on or off, then Ollama again.
func aiSetGPU(lay layout, on bool) sysReply {
	dir := aiDir(lay)
	unlock, err := aiLock(dir)
	if err != nil {
		return sysFail(errors.New("busy"))
	}
	defer unlock()
	marker := filepath.Join(dir, "cpu-only")
	if on {
		err = os.Remove(marker)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	} else {
		err = os.WriteFile(marker, nil, 0o600)
	}
	if err != nil {
		return sysFail(err)
	}
	if _, ours := aiOurs(dir); ours {
		aiStop(dir)
		if err := aiServe(dir); err != nil {
			return sysFail(err)
		}
	}
	return ok(nil)
}

// aiForget is "ai.forget": one model goes, the engine stays.
func aiForget(lay layout, model string) sysReply {
	if _, known := aiModelMB(model); !known {
		return sysFail(errors.New("unknown model"))
	}
	if aiBusy(aiDir(lay)) {
		return sysFail(errors.New("busy"))
	}
	b, _ := json.Marshal(map[string]string{"model": model})
	req, _ := http.NewRequest(http.MethodDelete, aiAPI+"/api/delete", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := aiQuick.Do(req)
	if err != nil {
		return sysFail(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return sysFail(errors.New(resp.Status))
	}
	return ok(nil)
}

// aiRemove is "ai.remove": Ollama stops, and it goes with every model.
// An Ollama the person installed is not touched.
func aiRemove(lay layout) sysReply {
	dir := aiDir(lay)
	if filepath.Base(dir) != aiFolder {
		return sysFail(errors.New("unexpected folder"))
	}
	unlock, err := aiLock(dir)
	if err != nil {
		return sysFail(errors.New("busy"))
	}
	defer unlock()
	aiStop(dir)
	if _, ours := aiOurs(dir); ours {
		return sysFail(errors.New("the local AI did not stop"))
	}
	for _, d := range []string{"ollama", "models", "new", "download.zip"} {
		if err := os.RemoveAll(filepath.Join(dir, d)); err != nil {
			return sysFail(err)
		}
	}
	_ = os.Remove(aiProgressFile(dir))
	return ok(nil)
}

// aiHost answers the ai.* commands of the System extension.
func aiHost(lay layout, req sysRequest) sysReply {
	switch req.Cmd {
	case "ai.status":
		return aiStatus(lay)
	case "ai.start":
		return aiStart(lay)
	}
	if !aiSupported() {
		return sysFail(errUnsupported)
	}
	if !aiMayManage(lay) {
		return sysFail(errors.New("owner only"))
	}
	switch req.Cmd {
	case "ai.install":
		return aiInstall(lay, req.Model)
	case "ai.forget":
		return aiForget(lay, req.Model)
	case "ai.gpu":
		return aiSetGPU(lay, req.On)
	case "ai.remove":
		return aiRemove(lay)
	}
	return sysFail(errUnsupported)
}

// --- orOS ai ---

type aiPin struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
}

// readAIPin reads the build's pin of the Ollama release; only GitHub
// release downloads of ollama/ollama are accepted.
func readAIPin(file string) (aiPin, error) {
	var p aiPin
	b, err := os.ReadFile(file)
	if err != nil {
		return p, fmt.Errorf("this edition has no local AI download (%v)", err)
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("%s: %v", file, err)
	}
	if !aiURLOK(p.URL) {
		return p, fmt.Errorf("unexpected download address %q", p.URL)
	}
	if s, err := hex.DecodeString(p.SHA256); err != nil || len(s) != sha256.Size {
		return p, errors.New("the local AI download has no valid SHA-256")
	}
	return p, nil
}

func aiURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" &&
		strings.HasPrefix(u.Path, "/ollama/ollama/releases/download/") &&
		(strings.HasSuffix(u.Path, "/ollama-linux-amd64.tar.zst") || strings.HasSuffix(u.Path, "/ollama-windows-amd64.zip"))
}

type aiOpts struct {
	dir, pin, progress, model string
}

// aiMain is "orOS ai --fetch [--model M] | --serve | --stop".
func aiMain(lay layout, args []string) int {
	fl := flag.NewFlagSet("ai", flag.ContinueOnError)
	o := aiOpts{}
	var fetch, serve, stop bool
	fl.BoolVar(&fetch, "fetch", false, "download and check Ollama (when missing), then the model")
	fl.BoolVar(&serve, "serve", false, "start Ollama in the background when it is installed")
	fl.BoolVar(&stop, "stop", false, "stop this orOS's Ollama")
	fl.StringVar(&o.model, "model", "", "model to download (qwen3:1.7b, qwen3:4b, qwen3:8b)")
	fl.StringVar(&o.dir, "dir", aiDir(lay), "folder for Ollama and its models")
	fl.StringVar(&o.pin, "pin", aiPinFile(lay), "the build's pin of the Ollama release (JSON)")
	fl.StringVar(&o.progress, "progress", "", "progress file (JSON; default <dir>/progress.json)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if o.progress == "" {
		o.progress = aiProgressFile(o.dir)
	}
	if stop {
		aiStop(o.dir)
		return 0
	}
	if serve {
		if err := aiServe(o.dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if !fetch {
		fmt.Fprintln(os.Stderr, "usage: orOS ai --fetch [--model qwen3:4b] [--dir D] | ai --serve | ai --stop")
		return 2
	}
	if o.model != "" {
		if _, known := aiModelMB(o.model); !known {
			fmt.Fprintln(os.Stderr, "unknown model", o.model)
			return 2
		}
	}
	report := aiWriter(o.progress)
	if err := aiFetch(o, func(step string, pct int) { report(aiState{Step: step, Percent: pct}) }); err != nil {
		report(aiState{Step: "error", Done: true, Error: err.Error()})
		log.Printf("ai: %v", err)
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(aiState{Step: "done", Percent: 100, Done: true})
	return 0
}

func aiFetch(o aiOpts, step func(string, int)) error {
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	unlock, err := aiLock(o.dir)
	if err != nil {
		return err
	}
	defer unlock()
	// An Ollama the person runs and that lets orOS in is used as it is.
	up, allowed := aiAnswers()
	if up && !allowed {
		return errAIForeign
	}
	engine := !up && !aiInstalled(o.dir)
	need := int64(0)
	if engine {
		need += 200
		if runtime.GOOS == "windows" {
			need += 1800 // the zip is kept until it is checked and unpacked
		}
	}
	if o.model != "" {
		mb, _ := aiModelMB(o.model)
		need += int64(mb) + 1024
	}
	if free := aiFreeMB(o.dir); free >= 0 && free < need {
		return fmt.Errorf("not enough free space: %d MB needed, %d MB free", need, free)
	}
	share := 0
	if engine {
		pin, err := readAIPin(o.pin)
		if err != nil {
			return err
		}
		log.Printf("ai: Ollama %s: %s", pin.Version, pin.URL)
		end := 100
		if o.model != "" {
			end = 30
		}
		if err := aiEngine(o.dir, pin, func(pct int) { step("engine", pct*end/100) }); err != nil {
			return err
		}
		share = end
	}
	if o.model == "" {
		return nil
	}
	step("start", share)
	if err := aiServe(o.dir); err != nil {
		return err
	}
	return aiPull(o.model, func(pct int) { step("model", share+pct*(99-share)/100) })
}

// aiKeep says which entries of an Ollama archive are kept, and where
// (under "ollama"): the program (bin/ollama in the Linux archive,
// ollama.exe in the Windows one), the libraries next to it (the processor
// variants for old and new processors among them) and the Vulkan folder
// for graphics cards, not the CUDA or ROCm folders, which are most of the
// download.
func aiKeep(name string) (string, bool) {
	name = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, `\`, "/")), "/")
	if name == "bin/ollama" || name == "ollama.exe" {
		return "ollama/" + name, true
	}
	if rest, ok := strings.CutPrefix(name, "lib/ollama/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "ollama/" + name, true
	}
	// Vulkan reaches most graphics cards (AMD, Intel, NVIDIA) through the
	// computer's own drivers.
	if rest, ok := strings.CutPrefix(name, "lib/ollama/vulkan/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "ollama/" + name, true
	}
	return "", false
}

// aiEngine downloads the pinned archive and installs what aiKeep keeps
// into <dir>/ollama, only when its SHA-256 matches. The Linux .tar.zst is
// unpacked while it streams (never kept on the disk); the Windows .zip
// is kept until it is checked, then unpacked.
func aiEngine(dir string, pin aiPin, pct func(int)) error {
	resp, err := aiHTTP.Get(pin.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", pin.URL, resp.Status)
	}
	if resp.ContentLength > aiMaxDownload {
		return fmt.Errorf("%s: too large", pin.URL)
	}
	h := sha256.New()
	counted := &countReader{r: io.LimitReader(resp.Body, aiMaxDownload+1), total: resp.ContentLength, pct: pct}
	body := io.TeeReader(counted, h)

	tmp := filepath.Join(dir, "new")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	check := func() error {
		if counted.n > aiMaxDownload {
			return fmt.Errorf("%s: too large", pin.URL)
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, pin.SHA256) {
			return fmt.Errorf("the Ollama download does not match its checksum (got %s)", got)
		}
		return nil
	}
	if strings.HasSuffix(pin.URL, ".zip") {
		zf := filepath.Join(dir, "download.zip")
		defer os.Remove(zf)
		f, err := os.Create(zf)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, body)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		if err := aiUnzip(zf, tmp); err != nil {
			return err
		}
	} else {
		if err := aiUntarZstd(body, tmp); err != nil {
			return err
		}
		// The rest of the stream counts for the checksum too.
		if _, err := io.Copy(io.Discard, body); err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
	}
	if fi, err := os.Stat(aiBinIn(tmp)); err != nil || (aiGOOS != "windows" && fi.Mode()&0o111 == 0) {
		return errors.New("the Ollama download has an unexpected layout")
	}
	cur := filepath.Join(dir, "ollama")
	_ = os.RemoveAll(cur)
	return os.Rename(filepath.Join(tmp, "ollama"), cur)
}

// aiPut writes one kept file below tmp, within the size budget.
func aiPut(tmp, name string, r io.Reader, size int64, exec bool, kept *int64) error {
	*kept += size
	if *kept > aiMaxKept {
		return errors.New("the Ollama download is larger than expected")
	}
	dst := filepath.Join(tmp, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, size))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != size {
		err = errors.New("the Ollama download ended early")
	}
	return err
}

func aiUntarZstd(r io.Reader, tmp string) error {
	dec, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer dec.Close()
	var kept int64
	tr := tar.NewReader(dec)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("unpacking Ollama: %v", err)
		}
		name, ok := aiKeep(hd.Name)
		if !ok {
			continue
		}
		switch hd.Typeflag {
		case tar.TypeReg:
			if err := aiPut(tmp, name, tr, hd.Size, hd.Mode&0o111 != 0, &kept); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Only links to a file in the same folder.
			target := strings.TrimPrefix(hd.Linkname, "./")
			if target == "" || strings.Contains(target, "/") || target == ".." {
				continue
			}
			dst := filepath.Join(tmp, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(target, dst); err != nil {
				return err
			}
		}
	}
	if _, err := io.Copy(io.Discard, dec); err != nil {
		return fmt.Errorf("unpacking Ollama: %v", err)
	}
	return nil
}

func aiUnzip(file, tmp string) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return fmt.Errorf("unpacking Ollama: %v", err)
	}
	defer zr.Close()
	var kept int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, ok := aiKeep(f.Name)
		if !ok {
			continue
		}
		if f.UncompressedSize64 > aiMaxKept {
			return errors.New("the Ollama download is larger than expected")
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("unpacking Ollama: %v", err)
		}
		err = aiPut(tmp, name, rc, int64(f.UncompressedSize64), strings.HasSuffix(name, "/bin/ollama"), &kept)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type countReader struct {
	r     io.Reader
	n     int64
	total int64
	pct   func(int)
	last  int
}

func (c *countReader) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	c.n += int64(k)
	if c.total > 0 && c.pct != nil {
		if v := int(c.n * 100 / c.total); v != c.last && v <= 100 {
			c.last = v
			c.pct(v)
		}
	}
	return k, err
}

// aiPull downloads a model through Ollama (from its own registry, where
// Ollama checks every layer's digest) and follows its progress.
func aiPull(model string, pct func(int)) error {
	b, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	resp, err := aiHTTP.Post(aiAPI+"/api/pull", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("model download: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	// The largest layer is the model's weights; its share is the progress.
	var big string
	var bigTotal int64
	for sc.Scan() {
		var m struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Error != "" {
			return fmt.Errorf("model download: %s", m.Error)
		}
		if m.Status == "success" {
			return nil
		}
		if m.Total > bigTotal {
			big, bigTotal = m.Digest, m.Total
		}
		if m.Digest == big && bigTotal > 0 {
			pct(int(m.Completed * 100 / bigTotal))
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("model download: %v", err)
	}
	return errors.New("model download ended early")
}
