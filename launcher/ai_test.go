package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// fakeOllamaTar is an archive laid out like ollama-linux-amd64.tar.zst.
func fakeOllamaTar(t *testing.T) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		must(t, tw.WriteHeader(h))
		if h.Typeflag == tar.TypeReg {
			io.WriteString(tw, body)
		}
	}
	add(&tar.Header{Name: "bin/ollama", Mode: 0o755}, "#!/bin/sh\necho ollama\n")
	add(&tar.Header{Name: "lib/ollama/libggml-cpu-sandybridge.so", Mode: 0o755}, "cpu")
	add(&tar.Header{Name: "lib/ollama/libggml-base.so.0.28.0", Mode: 0o755}, "base")
	add(&tar.Header{Name: "lib/ollama/libggml-base.so.0", Typeflag: tar.TypeSymlink, Linkname: "libggml-base.so.0.28.0"}, "")
	add(&tar.Header{Name: "lib/ollama/cuda_v12/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "lib/ollama/cuda_v12/libggml-cuda.so", Mode: 0o755}, strings.Repeat("g", 4096))
	add(&tar.Header{Name: "lib/ollama/vulkan/libggml-vulkan.so", Mode: 0o755}, "vk")
	add(&tar.Header{Name: "lib/ollama/evil", Typeflag: tar.TypeSymlink, Linkname: "../../../etc/passwd"}, "")
	add(&tar.Header{Name: "../escape", Mode: 0o644}, "no")
	tw.Close()
	var out bytes.Buffer
	zw, _ := zstd.NewWriter(&out)
	zw.Write(raw.Bytes())
	zw.Close()
	return out.Bytes()
}

// fakeOllamaZip is laid out like ollama-windows-amd64.zip.
func fakeOllamaZip(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, body := range map[string]string{
		"ollama.exe":                          "MZ",
		"lib/ollama/ggml-cpu-haswell.dll":     "cpu",
		"lib/ollama/LLAMA_CPP_LICENSE":        "MIT",
		"lib/ollama/cuda_v13/cublas64_13.dll": strings.Repeat("g", 4096),
		"lib/ollama/rocm/rocblas.dll":         strings.Repeat("r", 4096),
		"lib/ollama/vulkan/ggml-vulkan.dll":   "vk",
		"..\\evil.dll":                        "no",
	} {
		w, err := zw.Create(name)
		must(t, err)
		io.WriteString(w, body)
	}
	_, err := zw.Create("lib/ollama/vulkan/")
	must(t, err)
	zw.Close()
	return out.Bytes()
}

func serveBytes(t *testing.T, b []byte) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(b)))
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sumHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// noOllama points the commands at an address where nothing answers.
func noOllama(t *testing.T) {
	old := aiAPI
	aiAPI = "http://127.0.0.1:1"
	t.Cleanup(func() { aiAPI = old })
}

func withGOOS(t *testing.T, goos string) {
	old := aiGOOS
	aiGOOS = goos
	t.Cleanup(func() { aiGOOS = old })
}

func TestAIKeep(t *testing.T) {
	for name, want := range map[string]string{
		"bin/ollama":                          "ollama/bin/ollama",
		"./bin/ollama":                        "ollama/bin/ollama",
		"ollama.exe":                          "ollama/ollama.exe",
		`lib\ollama\ggml-base.dll`:            "ollama/lib/ollama/ggml-base.dll",
		"lib/ollama/libggml-cpu-x64.so":       "ollama/lib/ollama/libggml-cpu-x64.so",
		"lib/ollama/cuda_v13/libcublas.so.13": "",
		"lib/ollama/rocm/rocblas.dll":         "",
		"lib/ollama/vulkan/libggml-vulkan.so": "ollama/lib/ollama/vulkan/libggml-vulkan.so",
		"lib/ollama/vulkan/x/y.so":            "",
		"lib/ollama/":                         "",
		"../bin/ollama":                       "ollama/bin/ollama",
		"etc/passwd":                          "",
		"app/ollama app.exe":                  "",
	} {
		got, ok := aiKeep(name)
		if got != want || ok != (want != "") {
			t.Errorf("aiKeep(%q) = %q %v, want %q", name, got, ok, want)
		}
	}
}

func TestAIRecommend(t *testing.T) {
	for mem, want := range map[float64]string{3.7: "qwen3:1.7b", 7.6: "qwen3:4b", 11.5: "qwen3:4b", 15.4: "qwen3:8b", 31: "qwen3:8b"} {
		if got := aiRecommend(mem, 0); got != want {
			t.Errorf("aiRecommend(%v) = %s, want %s", mem, got, want)
		}
	}
	// A FAT32 stick cannot hold the 8b model's file.
	if got := aiRecommend(31, aiFAT32Max); got != "qwen3:4b" {
		t.Errorf("FAT32: %s", got)
	}
}

func TestAIPin(t *testing.T) {
	dir := t.TempDir()
	write := func(p aiPin) string {
		f := filepath.Join(dir, "pin.json")
		b, _ := json.Marshal(p)
		os.WriteFile(f, b, 0o644)
		return f
	}
	sum := strings.Repeat("ab", 32)
	for _, good := range []string{
		"https://github.com/ollama/ollama/releases/download/v0.40.14/ollama-linux-amd64.tar.zst",
		"https://github.com/ollama/ollama/releases/download/v0.40.14/ollama-windows-amd64.zip",
	} {
		if _, err := readAIPin(write(aiPin{"v0.40.14", good, sum})); err != nil {
			t.Fatal(good, err)
		}
	}
	for _, bad := range []aiPin{
		{"v", "http://github.com/ollama/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", sum},
		{"v", "https://example.com/ollama/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", sum},
		{"v", "https://github.com/someone/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", sum},
		{"v", "https://github.com/ollama/ollama/releases/download/v1/ollama-windows-amd64-rocm.zip", sum},
		{"v", "https://github.com/ollama/ollama/releases/download/v1/ollama-linux-amd64.tar.zst", "abc"},
	} {
		if _, err := readAIPin(write(bad)); err == nil {
			t.Errorf("pin %+v accepted", bad)
		}
	}
	if _, err := readAIPin(filepath.Join(dir, "none.json")); err == nil {
		t.Error("missing pin accepted")
	}
}

func TestAIEngineLinux(t *testing.T) {
	withGOOS(t, "linux")
	arc := fakeOllamaTar(t)
	srv := serveBytes(t, arc)
	dir := t.TempDir()
	last := -1
	if err := aiEngine(dir, aiPin{URL: srv.URL + "/ollama-linux-amd64.tar.zst", SHA256: sumHex(arc)}, func(p int) { last = p }); err != nil {
		t.Fatal(err)
	}
	if last != 100 {
		t.Errorf("progress ended at %d", last)
	}
	if !aiInstalled(dir) {
		t.Fatal("not installed")
	}
	for _, f := range []string{"lib/ollama/libggml-cpu-sandybridge.so", "lib/ollama/vulkan/libggml-vulkan.so"} {
		if _, err := os.Stat(filepath.Join(dir, "ollama", f)); err != nil {
			t.Error(err)
		}
	}
	if l, err := os.Readlink(filepath.Join(dir, "ollama/lib/ollama/libggml-base.so.0")); err != nil || l != "libggml-base.so.0.28.0" {
		t.Errorf("link %q %v", l, err)
	}
	for _, f := range []string{"ollama/lib/ollama/cuda_v12", "ollama/lib/ollama/evil", "escape", "new"} {
		if _, err := os.Lstat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s should not be there", f)
		}
	}
	// A download that does not match leaves nothing installed.
	other := t.TempDir()
	err := aiEngine(other, aiPin{URL: srv.URL + "/x.tar.zst", SHA256: strings.Repeat("00", 32)}, func(int) {})
	if err == nil || !strings.Contains(err.Error(), "checksum") || aiInstalled(other) {
		t.Fatalf("bad checksum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "new")); err == nil {
		t.Error("left new/ behind")
	}
}

func TestAIEngineWindows(t *testing.T) {
	withGOOS(t, "windows")
	arc := fakeOllamaZip(t)
	srv := serveBytes(t, arc)
	dir := t.TempDir()
	if err := aiEngine(dir, aiPin{URL: srv.URL + "/ollama-windows-amd64.zip", SHA256: sumHex(arc)}, func(int) {}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ollama.exe", "lib/ollama/ggml-cpu-haswell.dll", "lib/ollama/LLAMA_CPP_LICENSE", "lib/ollama/vulkan/ggml-vulkan.dll"} {
		if _, err := os.Stat(filepath.Join(dir, "ollama", f)); err != nil {
			t.Error(err)
		}
	}
	for _, f := range []string{"ollama/lib/ollama/cuda_v13", "ollama/lib/ollama/rocm", "evil.dll", "new", "download.zip"} {
		if _, err := os.Lstat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s should not be there", f)
		}
	}
	// The zip is checked before anything of it is unpacked.
	other := t.TempDir()
	err := aiEngine(other, aiPin{URL: srv.URL + "/x.zip", SHA256: strings.Repeat("00", 32)}, func(int) {})
	if err == nil || !strings.Contains(err.Error(), "checksum") || aiInstalled(other) {
		t.Fatalf("bad checksum: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "download.zip")); err == nil {
		t.Error("left the zip behind")
	}
}

func TestAIPull(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		for _, l := range []string{
			`{"status":"pulling manifest"}`,
			`{"status":"pulling a","digest":"sha256:a","total":1000,"completed":0}`,
			`{"status":"pulling b","digest":"sha256:b","total":10,"completed":10}`,
			`{"status":"pulling a","digest":"sha256:a","total":1000,"completed":500}`,
			`{"status":"pulling a","digest":"sha256:a","total":1000,"completed":1000}`,
			`{"status":"success"}`,
		} {
			fmt.Fprintln(w, l)
		}
	}))
	defer srv.Close()
	old := aiAPI
	aiAPI = srv.URL
	defer func() { aiAPI = old }()
	var steps []int
	if err := aiPull("qwen3:4b", func(p int) { steps = append(steps, p) }); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "qwen3:4b" || fmt.Sprint(steps) != "[0 50 100]" {
		t.Errorf("asked %v, progress %v", got, steps)
	}
}

// fakeServing answers like an Ollama that refuses other origins.
func fakeServing(t *testing.T, origins ...string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			allowed := false
			for _, a := range origins {
				allowed = allowed || a == o
			}
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		switch r.URL.Path {
		case "/api/version":
			io.WriteString(w, `{"version":"0.40.14"}`)
		case "/api/tags":
			io.WriteString(w, `{"models":[{"name":"qwen3:4b"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := aiAPI
	aiAPI = srv.URL
	t.Cleanup(func() { aiAPI = old })
}

func aiTestLayout(t *testing.T) layout {
	root := t.TempDir()
	return layout{root: root, data: filepath.Join(root, "data")}
}

func TestAIStatus(t *testing.T) {
	if !aiSupported() {
		t.Skip("no Ollama build for", runtime.GOOS, runtime.GOARCH)
	}
	noOllama(t)
	lay := aiTestLayout(t)
	st := aiStatus(lay)
	if st["ok"] != true || st["available"] != false || st["installed"] != false || st["persistent"] != true {
		t.Fatalf("no pin: %v", st)
	}
	if caps := sysStatus(lay, &fakeOps{})["caps"].(map[string]bool); caps["ai"] {
		t.Error("ai offered without a pin")
	}
	b, _ := json.Marshal(aiPin{"v0.40.14", "https://github.com/ollama/ollama/releases/download/v0.40.14/ollama-" +
		map[string]string{"windows": "windows-amd64.zip", "linux": "linux-amd64.tar.zst"}[runtime.GOOS], strings.Repeat("ab", 32)})
	must(t, os.WriteFile(aiPinFile(lay), b, 0o644))
	st = aiStatus(lay)
	if st["available"] != true || st["serving"] != nil || len(st["offer"].([]sysReply)) != 3 {
		t.Fatalf("pin: %v", st)
	}
	if caps := sysStatus(lay, &fakeOps{})["caps"].(map[string]bool); !caps["ai"] {
		t.Error("ai not offered with a pin")
	}
	// An Ollama the person runs that lets orOS in is used as it is.
	fakeServing(t, aiOrigin)
	st = aiStatus(lay)
	if st["serving"] != true || st["installed"] != true || fmt.Sprint(st["models"]) != "[qwen3:4b]" {
		t.Fatalf("serving: %v", st)
	}
	// One that turns orOS away is not used, and is not started over.
	fakeServing(t, "https://other.example")
	st = aiStatus(lay)
	if st["serving"] != nil || st["foreign"] != true {
		t.Fatalf("foreign: %v", st)
	}
	if r := aiStart(lay); r["ok"] != false || !strings.Contains(fmt.Sprint(r["error"]), "OLLAMA_ORIGINS") {
		t.Fatalf("start next to a foreign Ollama: %v", r)
	}
}

func TestAIInstallAndOwner(t *testing.T) {
	if !aiSupported() {
		t.Skip("no Ollama build here")
	}
	noOllama(t)
	lay := aiTestLayout(t)
	var ran []string
	old := aiSelf
	aiSelf = func(args ...string) error { ran = args; return nil }
	defer func() { aiSelf = old }()
	if r := aiHost(lay, sysRequest{Cmd: "ai.install", Model: "llama3:70b"}); r["ok"] != false {
		t.Errorf("unknown model: %v", r)
	}
	r := aiHost(lay, sysRequest{Cmd: "ai.install", Model: "qwen3:4b"})
	if r["ok"] != true || strings.Join(ran[:4], " ") != "ai --fetch --model qwen3:4b" {
		t.Fatalf("install: %v %v", r, ran)
	}
	// With users, only the owner adds or removes models; anyone may use them.
	owner := &orosUser{ID: "u0000000a", Name: "A", Owner: true}
	other := &orosUser{ID: "u0000000b", Name: "B", Dir: "users/u0000000b"}
	must(t, saveUsers(lay.data, &userList{V: 1, Users: []*orosUser{owner, other}}))
	if r := aiHost(lay.forUser(userDir(lay.data, other)), sysRequest{Cmd: "ai.remove"}); r["error"] != "owner only" {
		t.Errorf("other user removed: %v", r)
	}
	if r := aiHost(lay.forUser(userDir(lay.data, other)), sysRequest{Cmd: "ai.status"}); r["ok"] != true {
		t.Errorf("other user status: %v", r)
	}
	if r := aiHost(lay.forUser(userDir(lay.data, owner)), sysRequest{Cmd: "ai.remove"}); r["ok"] != true {
		t.Errorf("owner remove: %v", r)
	}
}

func TestAIFetchEngineOnly(t *testing.T) {
	noOllama(t)
	arc := fakeOllamaTar(t)
	withGOOS(t, "linux")
	srv := serveBytes(t, arc)
	old := aiURLOK
	aiURLOK = func(string) bool { return true }
	defer func() { aiURLOK = old }()
	tmp := t.TempDir()
	pin := filepath.Join(tmp, "pin.json")
	b, _ := json.Marshal(aiPin{"v-test", srv.URL + "/ollama-linux-amd64.tar.zst", sumHex(arc)})
	must(t, os.WriteFile(pin, b, 0o644))
	dir := filepath.Join(tmp, "ai")
	if code := aiMain(layout{root: tmp, data: tmp}, []string{"--fetch", "--dir", dir, "--pin", pin}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !aiInstalled(dir) {
		t.Fatal("not installed")
	}
	var p aiState
	pb, _ := os.ReadFile(aiProgressFile(dir))
	json.Unmarshal(pb, &p)
	if !p.Done || p.Error != "" || p.Percent != 100 {
		t.Errorf("progress %+v", p)
	}
	// While one setup runs, a second one stops at once.
	unlock, err := aiLock(dir)
	must(t, err)
	defer unlock()
	if !aiBusy(dir) {
		t.Error("not busy while locked")
	}
	if err := aiFetch(aiOpts{dir: dir, pin: pin, progress: aiProgressFile(dir)}, func(string, int) {}); err == nil {
		t.Error("second setup ran")
	}
	if code := aiMain(layout{root: tmp, data: tmp}, []string{"--fetch", "--model", "llama3:70b", "--dir", dir}); code != 2 {
		t.Errorf("unknown model: exit %d", code)
	}
}

// Stopping touches only this folder's Ollama.
func TestAIStopLeavesOthers(t *testing.T) {
	if _, ours := aiOurs(t.TempDir()); ours {
		t.Error("an Ollama from an empty folder")
	}
}
