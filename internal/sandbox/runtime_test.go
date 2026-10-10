package sandbox

import (
	"runtime"
	"strings"
	"testing"
)

func TestDockerSetupPlan(t *testing.T) {
	p := DockerSetupPlan()
	if p.Note == "" || p.Manual == "" {
		t.Errorf("plan missing Note/Manual: %+v", p)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(strings.ToLower(p.Note+p.Manual), "colima") {
		t.Errorf("darwin plan should mention Colima: %+v", p)
	}
	if runtime.GOOS == "linux" && !strings.Contains(p.Manual, "get.docker.com") {
		t.Errorf("linux plan should mention get.docker.com: %+v", p)
	}
}

func TestUpCommand(t *testing.T) {
	name, args := UpCommand()
	if name != "docker" {
		t.Errorf("name = %q, want docker", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"compose", "compose.sandbox.yaml", "up", "--build"} {
		if !strings.Contains(joined, want) {
			t.Errorf("up args %q missing %q", joined, want)
		}
	}
}
