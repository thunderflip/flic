//go:build js

package main

import (
	"os"
	"syscall"
)

// candidateInterruptSignals: js/wasm has no SIGHUP.
var candidateInterruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
