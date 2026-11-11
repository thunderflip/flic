//go:build !unix && !windows

package main

import "os/exec"

// isolateFromTerminalSignals does nothing on platforms without process groups
// or consoles; an interruption may then be reported as a failed verification.
func isolateFromTerminalSignals(cmd *exec.Cmd) {}
