//go:build !windows

package portable

import (
	"os/exec"
)

func isolate(cmd *exec.Cmd) {
	// leave the default process group: Setpgid made chrome hang under the
	// agent sandbox on macOS while a direct shell invocation succeeded.
}

func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
