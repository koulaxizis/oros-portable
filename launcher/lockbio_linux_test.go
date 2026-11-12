package main

import (
	"context"
	"errors"
	"testing"
)

func TestFprintAnswers(t *testing.T) {
	cases := map[string]string{
		"found 1 devices\nDevice at /net/reactivated/Fprint/Device/0\nUsing device /net/reactivated/Fprint/Device/0\nFingerprints for user oros on Goodix (press):\n - #0: right-index-finger\n": "ok",
		"found 1 devices\nDevice at /net/reactivated/Fprint/Device/0\nUser oros has no fingers enrolled for Goodix.\n":                                                                           "none",
		"No devices available\n": "no-hw",
		"":                       "no-hw",
	}
	for out, want := range cases {
		if got := fprintCan(out); got != want {
			t.Errorf("%q: %s, want %s", out, got, want)
		}
	}
	fail := errors.New("exit status 1")
	if fprintVerdict("Verify started!\nVerifying: right-index-finger\nVerify result: verify-match (done)\n", nil, false) != nil {
		t.Error("match")
	}
	for out, want := range map[string]string{
		"Verify result: verify-no-match (done)\n":              "failed",
		"No devices available\n":                               "no-hw",
		"No fingers enrolled for this device.\n":               "none",
		"failed to claim device: Device was already claimed\n": "busy",
	} {
		if err := fprintVerdict(out, fail, false); err == nil || err.Error() != want {
			t.Errorf("%q: %v, want %s", out, err, want)
		}
	}
	// "verify-match" printed but a failing exit is not a pass
	if fprintVerdict("verify-match", fail, false) == nil {
		t.Error("exit code ignored")
	}
	if err := fprintVerdict("", fail, true); err == nil || err.Error() != "timeout" {
		t.Errorf("timeout %v", err)
	}
}

func TestFprintVerifyRunsTheTool(t *testing.T) {
	old := fprintRun
	t.Cleanup(func() { fprintRun = old })
	var ran []string
	fprintRun = func(ctx context.Context, name string, args ...string) (string, error) {
		ran = append(ran, name)
		return "Verify result: verify-match (done)\n", nil
	}
	if err := (fprintBio{}).verify("Unlock orOS"); err != nil || len(ran) != 1 || ran[0] != "fprintd-verify" {
		t.Errorf("%v %v", err, ran)
	}
}
