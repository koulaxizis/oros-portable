package main

// Fingerprint for orOS's screen lock on Linux (lockbio.go): fprintd, the
// fingerprint service of GNOME, KDE and most desktops, through its own
// command line tools. "fprintd-list <user>" says whether a reader is there
// and a finger is enrolled; "fprintd-verify" waits for one touch (any
// enrolled finger) and exits 0 on a match. polkit lets the active local
// session verify its own fingers. D is kept readable by this user only.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

type fprintBio struct{}

func hostBio() bioOps { return fprintBio{} }

// fprintRun is replaced by tests.
var fprintRun = func(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := c.CombinedOutput()
	return string(out), err
}

var fprintLook = exec.LookPath

func (fprintBio) kind() string {
	if _, err := fprintLook("fprintd-verify"); err != nil {
		return ""
	}
	if _, err := fprintLook("fprintd-list"); err != nil {
		return ""
	}
	return "finger"
}

func fprintUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// fprintCan reads fprintd-list's answer (LANG=C).
func fprintCan(out string) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "no devices available"), strings.Contains(low, "impossible to"):
		return "no-hw"
	case strings.Contains(low, "#0:"), strings.Contains(low, " - #"):
		return "ok"
	case strings.Contains(low, "no fingers enrolled"), strings.Contains(low, "fingerprints for user"):
		return "none"
	}
	return "no-hw"
}

func (fprintBio) can() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, _ := fprintRun(ctx, "fprintd-list", fprintUser())
	return fprintCan(out)
}

// fprintVerdict reads fprintd-verify's answer: nil on verify-match.
func fprintVerdict(out string, err error, timedOut bool) error {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "verify-match") && err == nil:
		return nil
	case timedOut:
		return bioError("timeout")
	case strings.Contains(low, "no devices available"):
		return bioError("no-hw")
	case strings.Contains(low, "no fingers enrolled"), strings.Contains(low, "verify-no-enrolled"):
		return bioError("none")
	case strings.Contains(low, "already in use"), strings.Contains(low, "device was already claimed"):
		return bioError("busy")
	case strings.Contains(low, "verify-no-match"):
		return bioError("failed")
	}
	return bioError("failed")
}

func (fprintBio) verify(string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := fprintRun(ctx, "fprintd-verify")
	return fprintVerdict(out, err, errors.Is(ctx.Err(), context.DeadlineExceeded))
}

func (fprintBio) seal(d []byte) ([]byte, error) { return append([]byte(nil), d...), nil }

func (fprintBio) unseal(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
