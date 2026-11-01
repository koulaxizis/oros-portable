package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows: Tor Browser comes as its official installer (NSIS), which runs
// silently into the given folder (/S /D=…). It may leave shortcuts on the
// desktop and in the Start menu and an entry in Add or remove programs;
// those are taken away again (the person's own Tor Browser keeps its
// shortcuts and entry), so a stick leaves no trace of this kind on the
// computer.

var torPlatformKeys = []string{"windows-x86_64", "win64"}

const torDownloadExt = ".exe"

func torProgram(root string) string { return filepath.Join(root, "Browser", "firefox.exe") }

func torCommand(p string) *exec.Cmd {
	// The browser's launcher process would hand over and exit at once;
	// --wait-for-browser keeps it until the window closes.
	cmd := exec.Command(p, "--wait-for-browser")
	cmd.Dir = filepath.Dir(p)
	return cmd
}

var uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall`

func torInstall(file, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	// Shortcuts of a Tor Browser the person installed stay theirs: they
	// step aside while the installer runs and come back after it.
	links := torLinkFolders()
	kept := map[string]string{}
	for _, d := range links {
		for _, n := range dirNames(d) {
			if torLinkName(n) {
				p := filepath.Join(d, n)
				if os.Rename(p, p+".oros-keep") == nil {
					kept[p] = p + ".oros-keep"
				}
			}
		}
	}
	keysBefore := map[string]bool{}
	for _, k := range uninstallNames(registry.CURRENT_USER) {
		keysBefore["u"+k] = true
	}
	for _, k := range uninstallNames(registry.LOCAL_MACHINE) {
		keysBefore["m"+k] = true
	}

	cmd := exec.Command(file)
	// NSIS wants /D= last and unquoted, spaces and all.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    fmt.Sprintf(`"%s" /S /D=%s`, file, dest),
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Tor Browser's installer: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Minute):
		_ = cmd.Process.Kill()
		err = fmt.Errorf("took too long")
	}

	for _, d := range links {
		for _, n := range dirNames(d) {
			if torLinkName(n) {
				_ = os.RemoveAll(filepath.Join(d, n))
			}
		}
	}
	for p, k := range kept {
		_ = os.Rename(k, p)
	}
	for root, tag := range map[registry.Key]string{registry.CURRENT_USER: "u", registry.LOCAL_MACHINE: "m"} {
		for _, k := range uninstallNames(root) {
			if !keysBefore[tag+k] && uninstallPointsTo(root, k, dest) {
				_ = registry.DeleteKey(root, uninstallKey+`\`+k)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("Tor Browser's installer: %v", err)
	}
	return nil
}

// torLinkName: the shortcuts and Start menu folder Tor Browser's
// installer makes ("Tor Browser.lnk", "Tor Browser").
func torLinkName(n string) bool {
	return strings.HasPrefix(strings.ToLower(n), "tor browser") && !strings.HasSuffix(n, ".oros-keep")
}

// torLinkFolders: where an installer puts shortcuts, for this user and,
// when it runs as an administrator, for everyone.
func torLinkFolders() []string {
	var out []string
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_Desktop, windows.FOLDERID_Programs,
		windows.FOLDERID_PublicDesktop, windows.FOLDERID_CommonPrograms} {
		if p, err := windows.KnownFolderPath(id, 0); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func dirNames(d string) []string {
	f, err := os.Open(d)
	if err != nil {
		return nil
	}
	defer f.Close()
	n, _ := f.Readdirnames(-1)
	return n
}

func uninstallNames(root registry.Key) []string {
	k, err := registry.OpenKey(root, uninstallKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	n, _ := k.ReadSubKeyNames(-1)
	return n
}

func uninstallPointsTo(root registry.Key, name, dest string) bool {
	k, err := registry.OpenKey(root, uninstallKey+`\`+name, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	d := strings.ToLower(filepath.Clean(dest))
	for _, v := range []string{"InstallLocation", "UninstallString", "DisplayIcon"} {
		if s, _, err := k.GetStringValue(v); err == nil && strings.Contains(strings.ToLower(s), d) {
			return true
		}
	}
	return false
}
