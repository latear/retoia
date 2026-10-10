package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/latear/retoia/internal/config"
	"github.com/tidwall/gjson"
)

// TestPrintListing verifies the non-interactive --list output groups agents by
// section (Primary / Subagents) and models by category, so it can be used as a
// smoke test without opening the TUI.
func TestPrintListing(t *testing.T) {
	agents := []config.Agent{
		{Name: "Retoia-Orchestrator", Mode: "primary", CurrentModel: "model-a"},
		{Name: "cs-coder", Mode: "subagent", CurrentModel: "model-a"},
	}
	models := []config.Model{
		{ID: "model-a"}, {ID: "auto"},
		{ID: "esfuerzo-alto"}, {ID: "esfuerzo-bajo"},
		{ID: "cloud-kk"},
	}

	var buf bytes.Buffer
	printListing(&buf, agents, models)
	out := buf.String()

	for _, want := range []string{
		"Primary", "Subagents",
		"Retoia-Orchestrator", "model-a",
		"cs-coder",
		"Automatic", "Retoia", "By effort", "Cloud",
		"auto", "esfuerzo-bajo", "esfuerzo-alto", "cloud-kk",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("listing output is missing %q\n---\n%s", want, out)
		}
	}

	// Effort models must appear in level order (bajo before alto).
	if strings.Index(out, "esfuerzo-bajo") > strings.Index(out, "esfuerzo-alto") {
		t.Errorf("effort models out of order: bajo should precede alto\n---\n%s", out)
	}
}

// TestRun_UnknownCommand verifies an unrecognized subcommand errors out with
// a pointer to `retoia help`, instead of silently doing nothing.
func TestRun_UnknownCommand(t *testing.T) {
	err := run([]string{"bogus"})
	if err == nil {
		t.Fatal("run([\"bogus\"]) = nil error, want an error mentioning \"unknown command\"")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run([\"bogus\"]) error = %q, want it to mention %q", err.Error(), "unknown command")
	}
}

// TestRun_HelpAndNoArgs verifies that both an explicit help request and no
// arguments at all print usage and return no error (they are not failures).
func TestRun_HelpAndNoArgs(t *testing.T) {
	if err := run([]string{"help"}); err != nil {
		t.Errorf("run([\"help\"]) = %v, want nil", err)
	}
	if err := run(nil); err != nil {
		t.Errorf("run(nil) = %v, want nil", err)
	}
}

// TestRunCLI_HelpToStdout verifies that an explicit help request writes the
// usage text to the provided (stdout) writer, not stderr, so it can be piped
// or grepped. An unknown command must not print usage there; it returns an
// error for the caller to report on stderr instead.
func TestRunCLI_HelpToStdout(t *testing.T) {
	var out bytes.Buffer
	if err := runCLI(&out, []string{"help"}); err != nil {
		t.Fatalf("runCLI(help) = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "Usage: retoia") {
		t.Errorf("help usage not written to stdout writer; got %q", out.String())
	}

	out.Reset()
	if err := runCLI(&out, []string{"bogus"}); err == nil {
		t.Error("runCLI(bogus) = nil, want an error")
	}
	if out.Len() != 0 {
		t.Errorf("unknown command wrote %q to stdout; want nothing", out.String())
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to it. It is used to assert that no model name leaks to
// stdout on a gated, non-interactive path.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return buf.String()
}

// TestRun_ModelsList_RejectedKeyLeaksNoModels is the security-critical check
// for the gate: `models --list` with a key the gateway rejects must return an
// error and must NOT print any model name — even though the gateway is capable
// of serving models. It runs against an httptest gateway that answers 401 for
// every request, and sets RETOIA_API_KEY so the key resolves non-empty and
// the code path reaches validation (not the "no key" short-circuit).
func TestRun_ModelsList_RejectedKeyLeaksNoModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If a model name ever reaches stdout on this path, it would have to
		// come from here — but the key is rejected, so nothing is served.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv("RETOIA_API_KEY", "sk-bogus-rejected")

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"models", "--list",
			"--config", "testdata/opencode.sample.json",
			"--gateway", srv.URL})
	})

	if runErr == nil {
		t.Fatal("run(models --list) with a rejected key = nil error, want an error")
	}

	// No model id from the fixture (or any live model) may appear in stdout.
	for _, leaked := range []string{"model-a", "model-c", "auto", "cloud-", "Models ("} {
		if strings.Contains(out, leaked) {
			t.Errorf("rejected-key --list leaked %q to stdout:\n---\n%s", leaked, out)
		}
	}
}

// TestKeyStatus verifies the `retoia key` status text: env shadows the
// stored key, otherwise the stored key, otherwise none.
func TestKeyStatus(t *testing.T) {
	if s := keyStatus("sk-envvalue1234", "sk-stored1234"); !strings.Contains(s, "env") {
		t.Errorf("env should win; got %q", s)
	}
	if s := keyStatus("", "sk-stored1234"); !strings.Contains(s, "keychain") {
		t.Errorf("stored should show keychain; got %q", s)
	}
	if s := keyStatus("", ""); !strings.Contains(s, "none stored") {
		t.Errorf("empty should say none stored; got %q", s)
	}
	// Never leak a full key value.
	if s := keyStatus("sk-supersecretvalue", ""); strings.Contains(s, "supersecretvalue") {
		t.Errorf("must not print the full key: %q", s)
	}
}

// TestResolveUpdateBase covers the update endpoint precedence: the env var wins
// (trailing slash trimmed), otherwise the default endpoint.
func TestResolveUpdateBase(t *testing.T) {
	t.Setenv("RETOIA_UPDATE_BASE", "https://env.example/")
	if got := resolveUpdateBase(); got != "https://env.example" {
		t.Errorf("env override = %q, want https://env.example", got)
	}
}

// TestCmdMCP exercises the `retoia mcp` subcommand end to end against a temp
// config: adding a catalog server, a custom local server (with its own flags
// after `--`), a remote server, listing, and removing. It also checks the
// hand-rolled parser accepts a flag after the positional (mcp add id --config X).
func TestCmdMCP(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := cmdMCP(&buf, args); err != nil {
			t.Fatalf("cmdMCP %v: %v", args, err)
		}
		return buf.String()
	}

	// Catalog server that is wireable with no setup steps: --config comes AFTER
	// the positional id.
	if out := run("add", "context7", "--config", cfg); !strings.Contains(out, "context7") {
		t.Errorf("add context7 did not confirm the wire:\n%s", out)
	}
	// Custom local with the command (and its own --flag) after `--`.
	run("add", "mytool", "--config", cfg, "--", "uvx", "some-mcp", "--flag")
	// Remote server.
	run("add", "api", "--remote", "https://mcp.example/v1", "--config", cfg)

	data, _ := os.ReadFile(cfg)
	if got := gjson.GetBytes(data, "mcp.context7.url").String(); got == "" {
		t.Errorf("context7 url not written: %q", got)
	}
	if got := gjson.GetBytes(data, "mcp.mytool.command.2").String(); got != "--flag" {
		t.Errorf("custom command lost its --flag: got %q", got)
	}
	if got := gjson.GetBytes(data, "mcp.api.url").String(); got != "https://mcp.example/v1" {
		t.Errorf("api url = %q", got)
	}

	// List shows the configured servers and the catalog.
	list := run("list", "--config", cfg)
	for _, want := range []string{"context7", "mytool", "api", "Catalog"} {
		if !strings.Contains(list, want) {
			t.Errorf("list output missing %q:\n%s", want, list)
		}
	}

	// Remove.
	if out := run("remove", "context7", "--config", cfg); !strings.Contains(out, "removed") {
		t.Errorf("remove context7 output = %q", out)
	}
}

// TestCmdMCPErrors covers the error paths: unknown action, unknown catalog id,
// and a bad --env pair.
func TestCmdMCPErrors(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(cfg, []byte(`{"$schema":"x"}`), 0o644)
	var buf bytes.Buffer
	if err := cmdMCP(&buf, []string{"frobnicate"}); err == nil {
		t.Error("unknown action did not error")
	}
	if err := cmdMCP(&buf, []string{"add", "nope-not-real", "--config", cfg}); err == nil {
		t.Error("unknown catalog id did not error")
	}
	if err := cmdMCP(&buf, []string{"add", "t", "--env", "BADPAIR", "--config", cfg, "--", "uvx", "x"}); err == nil {
		t.Error("bad --env did not error")
	}
}

// TestCmdMCPSetup covers the setup command's validation paths. The pruned
// catalog's only entry (context7) needs no setup steps, so there is no
// wireable-with-setup-steps entry left to exercise a dry-run plan against;
// this covers the error paths only.
func TestCmdMCPSetup(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// A server with no setup steps is an error (use `add`).
	if err := cmdMCP(io.Discard, []string{"setup", "context7", "--config", cfg}); err == nil {
		t.Error("setup on a no-setup server should error")
	}
	// Unknown id errors.
	if err := cmdMCP(io.Discard, []string{"setup", "nope", "--config", cfg}); err == nil {
		t.Error("setup on unknown id should error")
	}
	// Unknown flag errors.
	if err := cmdMCP(io.Discard, []string{"setup", "context7", "--bogus"}); err == nil {
		t.Error("unknown flag should error")
	}
}

// TestCmdSandbox verifies the sandbox scaffolder writes its artifacts and prints
// the build steps.
func TestCmdSandbox(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := cmdSandbox(&buf, []string{"init", "--dir", dir}); err != nil {
		t.Fatalf("cmdSandbox: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile.sandbox")); err != nil {
		t.Errorf("Dockerfile.sandbox not written: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"compose.sandbox.yaml", "scaffolded"} {
		if !strings.Contains(out, want) {
			t.Errorf("sandbox output missing %q:\n%s", want, out)
		}
	}
	// open/shell without a compose file in the dir error clearly.
	if err := cmdSandbox(io.Discard, []string{"open", "--dir", t.TempDir()}); err == nil {
		t.Error("sandbox open without a compose file should error")
	}
	// Unknown action errors.
	if err := cmdSandbox(io.Discard, []string{"bogus"}); err == nil {
		t.Error("unknown sandbox action should error")
	}
}

// TestPurgeSandboxArtifacts verifies the scaffold is removed while the mounted
// work directory is preserved.
func TestPurgeSandboxArtifacts(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Dockerfile.sandbox", "compose.sandbox.yaml", "retoia-entrypoint.sh", "SANDBOX.md", ".dockerignore"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "opencode-config"), 0o755); err != nil {
		t.Fatal(err)
	}
	// sandbox-work holds the user's work and must survive the purge.
	if err := os.MkdirAll(filepath.Join(dir, "sandbox-work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sandbox-work", "loot.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed := purgeSandboxArtifacts(io.Discard, dir)
	if len(removed) != 6 {
		t.Errorf("removed %d artifacts, want 6: %v", len(removed), removed)
	}
	for _, n := range []string{"Dockerfile.sandbox", "compose.sandbox.yaml", "retoia-entrypoint.sh", "SANDBOX.md", ".dockerignore", "opencode-config"} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", n)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sandbox-work", "loot.txt")); err != nil {
		t.Errorf("sandbox-work must be preserved: %v", err)
	}
}

// TestSandboxModelsNeedsStagedConfig verifies `sandbox models` fails clearly when
// the sandbox has not been generated yet (no opencode-config/opencode.json).
func TestSandboxModelsNeedsStagedConfig(t *testing.T) {
	var buf bytes.Buffer
	err := cmdSandboxModels(&buf, []string{"--dir", t.TempDir(), "--no-build"})
	if err == nil {
		t.Fatal("expected an error when opencode-config is absent")
	}
	if !strings.Contains(err.Error(), "opencode-config") {
		t.Errorf("error should mention the missing staged config; got: %v", err)
	}
}
