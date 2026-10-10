package installtui

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func stubImpeccable(t *testing.T, ok bool, reason string) *int {
	t.Helper()
	calls := 0
	old := impeccableInstall
	impeccableInstall = func() (bool, string, error) { calls++; return ok, reason, nil }
	t.Cleanup(func() { impeccableInstall = old })
	return &calls
}

func TestApplyImpeccable(t *testing.T) {
	cases := []struct {
		name       string
		choose     bool
		ok         bool
		reason     string
		wantCalls  int
		wantInst   bool
		wantReason string
	}{
		{"not chosen", false, true, "", 0, false, ""},
		{"installed", true, true, "", 1, true, ""},
		{"skipped", true, false, "npx (Node.js) not found", 1, false, "npx (Node.js) not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			calls := stubImpeccable(t, c.ok, c.reason)
			cfg := filepath.Join(t.TempDir(), "opencode.json")
			res, err := apply(cfg, "retoia", "", setupResult{installImpeccable: c.choose}, keyResult{offline: true})
			if err != nil {
				t.Fatal(err)
			}
			if *calls != c.wantCalls || res.impeccableChosen != c.choose ||
				res.impeccableInstalled != c.wantInst || res.impeccableSkipReason != c.wantReason {
				t.Errorf("calls=%d res=%+v", *calls, res)
			}
		})
	}
}

func TestSummaryImpeccable(t *testing.T) {
	for name, tc := range map[string]struct {
		r    applyResult
		want string
	}{
		"installed": {applyResult{impeccableChosen: true, impeccableInstalled: true}, "installed"},
		"skipped":   {applyResult{impeccableChosen: true, impeccableSkipReason: "npx missing"}, "npx missing"},
		"unchosen":  {applyResult{}, "not selected"},
	} {
		t.Run(name, func(t *testing.T) {
			v := summaryModel{res: tc.r, width: 160, height: 60}.View()
			if !strings.Contains(v, "Impeccable") || !strings.Contains(v, tc.want) {
				t.Errorf("summary missing Impeccable/%q:\n%s", tc.want, v)
			}
		})
	}
}

func TestRunHeadlessImpeccable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := stubImpeccable(t, false, "no npx")
	var buf bytes.Buffer
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := RunHeadless(&buf, cfg, "retoia", "", HeadlessOptions{Impeccable: true}); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || !strings.Contains(buf.String(), "impeccable: skipped") {
		t.Errorf("calls=%d out=%q", *calls, buf.String())
	}
}
