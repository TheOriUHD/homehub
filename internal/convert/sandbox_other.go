//go:build !linux

package convert

import (
	"errors"
	"os/exec"
)

// The namespace sandbox is Linux-only; elsewhere external-tool conversions
// are refused unless the owner opts out with HOMEHUB_CONVERT_UNSANDBOXED=1.

func sandboxCommand(workDir string, argv []string, env []string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = env
	return cmd
}

func probeNamespaceSandbox() error {
	return errors.New("namespace sandbox requires Linux")
}
