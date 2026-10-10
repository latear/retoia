package installer

import (
	"fmt"
	"os"
	"os/exec"
)

// impeccableInstallArgs is the single source of truth for the official
// Impeccable design-skill install command (run through npx).
var impeccableInstallArgs = []string{"skills", "add", "pbakaus/impeccable"}

// ImpeccableManualCmd is the command a user can run by hand when the automatic
// install is skipped.
const ImpeccableManualCmd = "npx skills add pbakaus/impeccable"

// InstallImpeccableSkill installs the Impeccable design skill through its
// official CLI (npx skills add pbakaus/impeccable). It is best-effort: a missing
// npx or a failing command yields installed=false with a human-readable
// skipReason and a nil error, so callers can degrade to a skip. err is reserved
// for unexpected conditions and is currently always nil.
func InstallImpeccableSkill() (installed bool, skipReason string, err error) {
	return installImpeccable(exec.LookPath, func(npx string) error {
		cmd := exec.Command(npx, impeccableInstallArgs...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	})
}

// installImpeccable holds the decision logic with injectable seams so it can be
// tested without Node or the network.
func installImpeccable(lookPath func(string) (string, error), run func(npx string) error) (bool, string, error) {
	npx, lerr := lookPath("npx")
	if lerr != nil {
		return false, "npx (Node.js) not found; install Impeccable manually with: " + ImpeccableManualCmd, nil
	}
	if rerr := run(npx); rerr != nil {
		return false, fmt.Sprintf("Impeccable install failed (%v); install it manually with: %s", rerr, ImpeccableManualCmd), nil
	}
	return true, "", nil
}
