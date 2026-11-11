//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// isolateFromTerminalSignals starts the command in its own process group. The
// terminal sends Ctrl+C (SIGINT) and Ctrl+\ (SIGQUIT) to the foreground process
// group only, so they reach flic but not flac. flac's standard input is
// /dev/null and its output goes to pipes, so being outside the foreground group
// never stops it with SIGTTIN or SIGTTOU.
func isolateFromTerminalSignals(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
