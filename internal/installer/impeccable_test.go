package installer

import (
	"errors"
	"strings"
	"testing"
)

func TestInstallImpeccable_SkipsWhenNpxMissing(t *testing.T) {
	ran := false
	ok, reason, err := installImpeccable(
		func(string) (string, error) { return "", errors.New("not found") },
		func(string) error { ran = true; return nil })
	if err != nil || ok || ran {
		t.Fatalf("ok=%v ran=%v err=%v, want skipped without running", ok, ran, err)
	}
	if !strings.Contains(reason, "npx") || !strings.Contains(reason, ImpeccableManualCmd) {
		t.Errorf("reason = %q", reason)
	}
}

func TestInstallImpeccable_RunFailureIsSkip(t *testing.T) {
	ok, reason, err := installImpeccable(
		func(string) (string, error) { return "/usr/bin/npx", nil },
		func(string) error { return errors.New("exit status 1") })
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want skip", ok, err)
	}
	if !strings.Contains(reason, "exit status 1") || !strings.Contains(reason, ImpeccableManualCmd) {
		t.Errorf("reason = %q", reason)
	}
}

func TestInstallImpeccable_Success(t *testing.T) {
	var got string
	ok, reason, err := installImpeccable(
		func(string) (string, error) { return "/usr/bin/npx", nil },
		func(npx string) error { got = npx; return nil })
	if err != nil || !ok || reason != "" || got != "/usr/bin/npx" {
		t.Fatalf("ok=%v reason=%q err=%v npx=%q", ok, reason, err, got)
	}
}

func TestImpeccableCommandSingleSource(t *testing.T) {
	if want := "npx " + strings.Join(impeccableInstallArgs, " "); want != ImpeccableManualCmd {
		t.Errorf("args %q drift from manual cmd %q", want, ImpeccableManualCmd)
	}
}
