package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DockerReady reports whether the docker CLI is on PATH AND its daemon answers.
// A present CLI with no running daemon (e.g. Docker Desktop not launched, or
// Colima not started) counts as not ready.
func DockerReady() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	return exec.Command("docker", "info").Run() == nil
}

// SetupPlan describes how to get a Docker daemon on the current OS without
// Docker Desktop. Auto is a shell command that sets it up unattended when
// possible (empty when it cannot be automated); Note is the human summary and
// Manual the steps to show when Auto is empty or fails.
type SetupPlan struct {
	Auto   string // shell command to run (via `bash -lc`); empty = cannot automate
	Note   string // one-line description of the approach
	Manual string // instructions to show the user
}

// DockerSetupPlan returns the OS-appropriate way to provide a Docker daemon.
// macOS uses Colima (CLI-only, no Docker Desktop); Linux uses get.docker.com.
func DockerSetupPlan() SetupPlan {
	switch runtime.GOOS {
	case "darwin":
		// Start Colima with enough memory for the Kali image build; the 2 GB
		// default OOM-kills it (exit 137).
		const colimaStart = "colima start --cpus 4 --memory 6"
		if _, err := exec.LookPath("brew"); err == nil {
			return SetupPlan{
				Auto:   "brew install colima docker docker-compose && " + colimaStart,
				Note:   "Colima (a lightweight Docker daemon for macOS — no Docker Desktop)",
				Manual: "Install Homebrew, then: brew install colima docker docker-compose && " + colimaStart,
			}
		}
		return SetupPlan{
			Note:   "Colima (no Docker Desktop) needs Homebrew",
			Manual: "Install Homebrew (brew.sh), then: brew install colima docker docker-compose && " + colimaStart,
		}
	case "linux":
		return SetupPlan{
			Auto:   "curl -fsSL https://get.docker.com | sh",
			Note:   "Docker Engine via get.docker.com (needs sudo/root)",
			Manual: "Run as root: curl -fsSL https://get.docker.com | sh   (then add your user to the docker group)",
		}
	default:
		return SetupPlan{
			Note:   "unsupported OS for automatic Docker setup",
			Manual: "Install Docker for your OS and ensure the daemon is running.",
		}
	}
}

// DockerMemGB returns the Docker daemon's total memory in GB, or 0 if it can't
// be determined. Used to warn before a build that may OOM on a small VM.
func DockerMemGB() float64 {
	out, err := exec.Command("docker", "info", "--format", "{{.MemTotal}}").Output()
	if err != nil {
		return 0
	}
	bytes, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return bytes / (1024 * 1024 * 1024)
}

// UpCommand is the command that builds and starts the sandbox container from the
// scaffolded compose file (run from the repo root).
func UpCommand() (name string, args []string) {
	// Plain build output is forced with BUILDKIT_PROGRESS=plain in the environment
	// (see BuildEnv), not a `--progress` flag: the global compose flag is rejected
	// by some Docker versions ("unknown flag: --progress"), while the env var is
	// portable. Plain output matters because the default spinner UI renders as a
	// frozen blank over many SSH terminals.
	return "docker", []string{"compose", "-f", "compose.sandbox.yaml", "up", "-d", "--build"}
}

// BuildEnv is the extra environment for the sandbox build: force plain BuildKit
// progress so the output renders line-by-line on any terminal.
func BuildEnv() []string { return []string{"BUILDKIT_PROGRESS=plain"} }

// ImageExists reports whether the retoia-sandbox image is already built.
func ImageExists() bool {
	out, err := exec.Command("docker", "images", "-q", "retoia-sandbox").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// ContainerUp reports whether a retoia sandbox container is currently running.
func ContainerUp() bool {
	out, err := exec.Command("docker", "ps", "-q", "--filter", "name=retoia").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// ImageVersion returns the retoia version stamped on the built image (the
// retoia.version label), or "" when unavailable.
func ImageVersion() string {
	out, err := exec.Command("docker", "image", "inspect", "retoia-sandbox",
		"--format", `{{index .Config.Labels "retoia.version"}}`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ImageAge returns a short human hint of how old the built image is (e.g. "3h
// ago", "2d ago"), or "" when unavailable.
func ImageAge() string {
	out, err := exec.Command("docker", "image", "inspect", "retoia-sandbox",
		"--format", "{{.Created}}").Output()
	if err != nil {
		return ""
	}
	t, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(out)))
	if perr != nil {
		return ""
	}
	d := time.Since(t)
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
