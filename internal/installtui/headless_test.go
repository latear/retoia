package installtui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// TestRunHeadless covers the non-interactive install with no gateway key: it
// must create a fresh opencode.json, reusing apply() without any TUI. HOME and
// OpenCode paths are redirected to a temp dir so the test never touches the
// real environment.
func TestRunHeadless(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCODE_AUTH_PATH", filepath.Join(home, "auth.json"))
	t.Setenv("RETOIA_API_KEY", "")

	cfg := filepath.Join(home, "opencode.json") // does not exist yet

	var buf bytes.Buffer
	err := RunHeadless(&buf, cfg, "retoia", "", HeadlessOptions{Engram: false})
	if err != nil {
		t.Fatalf("RunHeadless: %v", err)
	}

	data, rerr := os.ReadFile(cfg)
	if rerr != nil {
		t.Fatalf("reading config: %v", rerr)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("result is not valid JSON")
	}
	if !strings.Contains(buf.String(), "headless install done") {
		t.Errorf("summary missing:\n%s", buf.String())
	}
}

// TestRunHeadlessWiresContext7 covers the Context7 option: it wires the
// context7 docs MCP (mcp.context7) into opencode.json.
func TestRunHeadlessWiresContext7(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCODE_AUTH_PATH", filepath.Join(home, "auth.json"))
	t.Setenv("RETOIA_API_KEY", "")

	cfg := filepath.Join(home, "opencode.json")
	var buf bytes.Buffer
	if err := RunHeadless(&buf, cfg, "retoia", "", HeadlessOptions{Context7: true}); err != nil {
		t.Fatalf("RunHeadless: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if got := gjson.GetBytes(data, "mcp.context7.type").String(); got != "remote" {
		t.Errorf("context7 not wired: type=%q", got)
	}
}

// TestRunHeadlessNoGatewaySkipsProvider verifies that with no gateway the
// install does NOT write an (empty) provider block, and reports the
// no-gateway reminder.
func TestRunHeadlessNoGatewaySkipsProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCODE_AUTH_PATH", filepath.Join(home, "auth.json"))
	t.Setenv("RETOIA_API_KEY", "")

	cfg := filepath.Join(home, "opencode.json")
	var buf bytes.Buffer
	if err := RunHeadless(&buf, cfg, "retoia", "", HeadlessOptions{}); err != nil {
		t.Fatalf("RunHeadless: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if gjson.GetBytes(data, "provider.retoia.options.baseURL").Exists() {
		t.Error("no empty provider baseURL should be written when there is no gateway")
	}
	if !strings.Contains(buf.String(), "gateway: none") {
		t.Errorf("expected the no-gateway reminder:\n%s", buf.String())
	}
}

// TestStageSandboxConfig covers generating the baked sandbox config: it writes
// opencode.json + tui.json into cfgDir and does NOT include the gateway
// credential (auth.json), which the container writes at start from the env.
func TestStageSandboxConfig(t *testing.T) {
	t.Setenv("RETOIA_API_KEY", "")
	cfgDir := filepath.Join(t.TempDir(), "opencode-config")

	if err := stageSandboxConfig(cfgDir, "", "", "", defaultSandboxExtras()); err != nil {
		t.Fatalf("stageSandboxConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfgDir, "opencode.json"))
	if err != nil {
		t.Fatalf("opencode.json not generated: %v", err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("baked opencode.json is not valid JSON")
	}
	if got := gjson.GetBytes(data, "mcp.engram.type").String(); got != "local" {
		t.Errorf("engram MCP not baked: type=%q", got)
	}
	if got := gjson.GetBytes(data, "mcp.context7.type").String(); got != "remote" {
		t.Errorf("context7 MCP not baked: type=%q", got)
	}
	tuiData, terr := os.ReadFile(filepath.Join(cfgDir, "tui.json"))
	if terr != nil {
		t.Errorf("tui.json not baked: %v", terr)
	}
	// The logo plugin must be registered with the CONTAINER path, not the
	// throwaway stage HOME (which does not exist inside the container).
	if !strings.Contains(string(tuiData), "/root/.config/opencode/tui-plugins/retoia-logo.tsx") {
		t.Errorf("logo plugin not registered at the container path; tui.json=%s", tuiData)
	}
	if strings.Contains(string(tuiData), "retoia-sandbox-stage-") {
		t.Errorf("tui.json still references the throwaway stage path: %s", tuiData)
	}
	// The gateway credential must NOT be baked into the config dir.
	if _, err := os.Stat(filepath.Join(cfgDir, "auth.json")); err == nil {
		t.Error("auth.json should not be in the baked config (written at container start)")
	}
}

// TestInheritCompatibleMCP verifies the sandbox inherits only runnable host MCP
// servers: remote ones and local ones launched by a container runtime; docker
// and absolute-path/junk commands are skipped.
func TestInheritCompatibleMCP(t *testing.T) {
	dir := t.TempDir()
	host := filepath.Join(dir, "host.json")
	os.WriteFile(host, []byte(`{"mcp":{
		"context7":{"type":"remote","url":"https://mcp.context7.com/mcp"},
		"realnpx":{"type":"local","command":["npx","-y","some-mcp"]},
		"junk":{"type":"local","command":["--config","/tmp/x/opencode.json"]},
		"dockerone":{"type":"local","command":["docker","run","x"]}
	}}`), 0o644)
	stage := filepath.Join(dir, "opencode.json")
	os.WriteFile(stage, []byte(`{"$schema":"x"}`), 0o644)

	inheritCompatibleMCP(stage, host)

	data, _ := os.ReadFile(stage)
	got := map[string]bool{}
	gjson.GetBytes(data, "mcp").ForEach(func(k, _ gjson.Result) bool { got[k.String()] = true; return true })
	for _, want := range []string{"context7", "realnpx"} {
		if !got[want] {
			t.Errorf("expected %q inherited; got %v", want, got)
		}
	}
	for _, no := range []string{"junk", "dockerone"} {
		if got[no] {
			t.Errorf("%q should NOT be inherited (not runnable in the container)", no)
		}
	}
}
