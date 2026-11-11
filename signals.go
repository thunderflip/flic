//go:build !js

package main

import (
	"os"
	"syscall"
)

// candidateInterruptSignals: on Windows, SIGHUP is never delivered and is harmless.
var candidateInterruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
