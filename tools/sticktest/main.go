// sticktest measures how well a USB stick will run orOS Portable.
//
// Copy it onto the stick and run it there (double-click on Windows), or pass
// a folder on the stick. It measures sequential write and read, random 4 KiB
// writes that wait for the disk (what the browser profile does all the time)
// and small-file creation, then prints a verdict and appends the numbers to
// sticktest-results.txt next to the test folder.
//
// Usage:
//
//	sticktest [-name "Brand model 32GB"] [-size 512] [-seconds 20] [folder]
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var version = "0.0.0-dev"

type config struct {
	dir     string
	sizeMiB int
	seconds float64
	files   int
}

type result struct {
	SeqWriteMBs float64
	SeqReadMBs  float64
	RandIOPS    float64
	FilesPerSec float64
}

// Guidance thresholds. The random synced 4 KiB write rate decides most of
// how the browser feels on the stick; cheap sticks often manage under 10.
type grade struct {
	label     string
	good, ok  float64
	value     func(result) float64
	unit      string
	explained string
}

var grades = []grade{
	{"Sequential write", 30, 10, func(r result) float64 { return r.SeqWriteMBs }, "MB/s", "installing and updating orOS"},
	{"Sequential read", 80, 30, func(r result) float64 { return r.SeqReadMBs }, "MB/s", "starting the browser"},
	{"Random 4K synced writes", 150, 40, func(r result) float64 { return r.RandIOPS }, "IOPS", "everyday browser speed"},
	{"Small files", 100, 30, func(r result) float64 { return r.FilesPerSec }, "files/s", "saving data and caches"},
}

func main() {
	name := flag.String("name", "", "name of the stick for the results file")
	size := flag.Int("size", 512, "size of the sequential test file in MiB")
	secs := flag.Float64("seconds", 20, "duration of the random write test")
	flag.Parse()

	dir := flag.Arg(0)
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			fail(err)
		}
		dir = filepath.Dir(exe)
	}
	fmt.Printf("orOS stick test %s\nfolder: %s\n\n", version, dir)
	if *name == "" {
		fmt.Print("Name of this stick (brand, model, size), then Enter: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		*name = strings.TrimSpace(line)
	}

	cfg := config{dir: dir, sizeMiB: *size, seconds: *secs, files: 300}
	r, err := run(cfg, func(s string) { fmt.Println(s) })
	if err != nil {
		fail(err)
	}
	report := format(*name, r)
	fmt.Println()
	fmt.Print(report)
	if err := appendResults(filepath.Join(dir, "sticktest-results.txt"), report); err != nil {
		fmt.Println("could not save results:", err)
	}
	pause()
}

func fail(err error) {
	fmt.Println("error:", err)
	pause()
	os.Exit(1)
}

// pause keeps the console open when the program was double-clicked on Windows.
func pause() {
	if runtime.GOOS == "windows" {
		fmt.Print("\nPress Enter to close.")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}

func run(cfg config, progress func(string)) (result, error) {
	var r result
	work, err := os.MkdirTemp(cfg.dir, "sticktest-tmp-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(work)

	big := filepath.Join(work, "seq.bin")
	progress(fmt.Sprintf("1/4 writing %d MiB...", cfg.sizeMiB))
	if r.SeqWriteMBs, err = seqWrite(big, cfg.sizeMiB); err != nil {
		return r, err
	}
	progress("2/4 reading it back...")
	if r.SeqReadMBs, err = seqRead(big); err != nil {
		return r, err
	}
	_ = os.Remove(big)
	progress(fmt.Sprintf("3/4 random 4 KiB synced writes for %.0f s...", cfg.seconds))
	if r.RandIOPS, err = randWrites(filepath.Join(work, "rand.bin"), cfg.seconds); err != nil {
		return r, err
	}
	progress(fmt.Sprintf("4/4 creating %d small files...", cfg.files))
	if r.FilesPerSec, err = smallFiles(filepath.Join(work, "small"), cfg.files); err != nil {
		return r, err
	}
	return r, nil
}

const chunk = 4 << 20

func mbs(bytes int64, d time.Duration) float64 {
	return float64(bytes) / 1e6 / d.Seconds()
}

func seqWrite(path string, sizeMiB int) (float64, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf := make([]byte, chunk)
	rand.New(rand.NewSource(1)).Read(buf) // incompressible, some controllers compress
	start := time.Now()
	var n int64
	for n < int64(sizeMiB)<<20 {
		w, err := f.Write(buf)
		if err != nil {
			return 0, noSpace(err)
		}
		n += int64(w)
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	return mbs(n, time.Since(start)), nil
}

func seqRead(path string) (float64, error) {
	f, err := openUncached(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf := alignedBuf(chunk)
	start := time.Now()
	var n int64
	for {
		r, err := f.Read(buf)
		n += int64(r)
		if errors.Is(err, io.EOF) || (err == nil && r == 0) {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	return mbs(n, time.Since(start)), nil
}

func randWrites(path string, seconds float64) (float64, error) {
	const span = 64 << 20
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := f.Truncate(span); err != nil {
		return 0, noSpace(err)
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	rnd := rand.New(rand.NewSource(2))
	buf := make([]byte, 4096)
	rnd.Read(buf)
	limit := time.Duration(seconds * float64(time.Second))
	start := time.Now()
	ops := 0
	for time.Since(start) < limit {
		off := int64(rnd.Intn(span/4096)) * 4096
		if _, err := f.WriteAt(buf, off); err != nil {
			return 0, noSpace(err)
		}
		if err := f.Sync(); err != nil {
			return 0, err
		}
		ops++
	}
	return float64(ops) / time.Since(start).Seconds(), nil
}

func smallFiles(dir string, count int) (float64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	rnd := rand.New(rand.NewSource(3))
	buf := make([]byte, 32<<10)
	rnd.Read(buf)
	start := time.Now()
	for i := 0; i < count; i++ {
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("f%04d.bin", i)))
		if err != nil {
			return 0, noSpace(err)
		}
		_, err = f.Write(buf[:8<<10+rnd.Intn(24<<10)])
		if err == nil {
			err = f.Sync()
		}
		f.Close()
		if err != nil {
			return 0, noSpace(err)
		}
	}
	return float64(count) / time.Since(start).Seconds(), nil
}

func noSpace(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "space") {
		return fmt.Errorf("%w (the test needs about 600 MiB free)", err)
	}
	return err
}

func rate(g grade, v float64) string {
	switch {
	case v >= g.good:
		return "good"
	case v >= g.ok:
		return "ok"
	default:
		return "poor"
	}
}

func verdict(r result) string {
	worst := "good"
	for _, g := range grades {
		switch rate(g, g.value(r)) {
		case "poor":
			worst = "poor"
		case "ok":
			if worst == "good" {
				worst = "ok"
			}
		}
	}
	switch worst {
	case "good":
		return "GOOD for orOS Portable"
	case "ok":
		return "OK for orOS Portable, a little slow"
	}
	return "POOR: orOS Portable will feel slow on this stick"
}

func format(name string, r result) string {
	var b strings.Builder
	if name == "" {
		name = "(unnamed stick)"
	}
	fmt.Fprintf(&b, "%s  %s  %s/%s\n", time.Now().Format("2006-01-02 15:04"), name, runtime.GOOS, runtime.GOARCH)
	for _, g := range grades {
		v := g.value(r)
		fmt.Fprintf(&b, "  %-25s %8.1f %-8s %-5s (%s)\n", g.label, v, g.unit, rate(g, v), g.explained)
	}
	fmt.Fprintf(&b, "  Verdict: %s\n\n", verdict(r))
	return b.String()
}

func appendResults(path, report string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(report)
	return err
}
