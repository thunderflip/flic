//go:build windows

package flac

import (
	"os/exec"
	"syscall"
)

// detachedProcess is the Win32 DETACHED_PROCESS creation flag.
const detachedProcess = 0x00000008

// isolateFromTerminalSignals starts the command without a console. Console
// control events (Ctrl+C, Ctrl+Break, closing the window) are only delivered to
// processes attached to the console, so they reach flic but not flac. flac's
// output is still captured through pipes.
func isolateFromTerminalSignals(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
}
