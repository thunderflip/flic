//go:build unix

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func init() {
	fakeFlacModes["pgrp"] = func() { fmt.Print(syscall.Getpgrp()) }
}

func TestFlacRunsInOwnProcessGroup(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), os.Args[0])
	cmd.Env = append(os.Environ(), "FLIC_FAKE_FLAC=pgrp")
	isolateFromTerminalSignals(cmd)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("unexpected output %q", output)
	}
	if child == syscall.Getpgrp() {
		t.Fatalf("flac shares flic's process group %d", child)
	}
	if child != cmd.Process.Pid {
		t.Fatalf("process group %d, want flac's own PID %d", child, cmd.Process.Pid)
	}
}
