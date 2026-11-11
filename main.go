package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultMaxMissingPercentage = 50

const optionNotSet = math.MinInt32

// exitInterrupted follows the shell convention for a run stopped by Ctrl+C (128 + SIGINT).
const exitInterrupted = 130

// interruptSignals lists the signals that stop a run cleanly: Ctrl+C, termination
// requests (including closing the console window on Windows), and the hangup sent
// when a Unix terminal or SSH session closes. Signals that were ignored when flic
// started (for example SIGHUP under nohup) stay ignored.
func interruptSignals() []os.Signal {
	var signals []os.Signal
	// On Windows, SIGHUP is never delivered and is harmless.
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signals = append(signals, sig)
		}
	}
	return signals
}

func main() { os.Exit(run()) }

func run() int {
	flacPath := flag.String("flac", "", "Path to the flac executable")
	folder := flag.String("folder", "", "Root folder path to FLAC collection")
	report := flag.String("report", "", "Path to the report file")
	age := flag.Int("age", optionNotSet, "Age in minutes (-1: never checked, -2: before today)")
	minPercent := flag.Int("min-percentage", optionNotSet, "Minimum percentage of collection to check")
	maxPercent := flag.Int("max-percentage", optionNotSet, "Maximum percentage of collection to check")
	maxMissingPercent := flag.Int("max-missing-percentage", defaultMaxMissingPercentage, "Maximum percentage of report entries that may be missing")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected argument %q; options must use the --name=value form\n", flag.Arg(0))
		return 2
	}
	if err := validateOptions(*flacPath, *folder, *report, *age, *minPercent, *maxPercent, *maxMissingPercent); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 2
	}
	ctx, stop := context.Background(), func() {}
	// With an empty list, NotifyContext would capture every signal.
	if signals := interruptSignals(); len(signals) > 0 {
		ctx, stop = signal.NotifyContext(context.Background(), signals...)
	}
	defer stop()
	// Deferred calls run in reverse order: done is closed before stop cancels ctx.
	done := make(chan struct{})
	defer close(done)
	go announceInterruption(ctx, done, stop, os.Stderr)
	if err := check(ctx, *flacPath, *folder, *report, *age, *minPercent, *maxPercent, *maxMissingPercent); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if errors.Is(err, errInterrupted) {
			return exitInterrupted
		}
		return 1
	}
	return 0
}

// announceInterruption waits for an interruption signal (ctx cancelled), then
// restores default signal handling, so that a second Ctrl+C terminates
// immediately, and tells the user that progress is being saved. It stays silent
// when ctx is only cancelled because the run is over (done already closed).
func announceInterruption(ctx context.Context, done <-chan struct{}, stop func(), w io.Writer) {
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	select {
	case <-done:
		return
	default:
	}
	stop()
	fmt.Fprintln(w, "Interruption requested; saving progress and releasing the report lock...")
}

func validateOptions(flac, folder, report string, age, minPercent, maxPercent, maxMissingPercent int) error {
	if flac == "" || folder == "" || report == "" {
		return errors.New("--flac, --folder and --report are required")
	}
	if minPercent != optionNotSet && maxPercent != optionNotSet {
		return errors.New("--min-percentage and --max-percentage cannot be combined")
	}
	if age == optionNotSet && minPercent == optionNotSet {
		return errors.New("specify --age or --min-percentage; --max-percentage requires --age")
	}
	if age != optionNotSet && (age < -2 || int64(age) > int64(math.MaxInt64)/int64(time.Minute)) {
		return errors.New("--age must be -2, -1, or a non-negative number of minutes within the supported duration")
	}
	for _, p := range []int{minPercent, maxPercent} {
		if p != optionNotSet && (p < 0 || p > 100) {
			return errors.New("percentages must be between 0 and 100")
		}
	}
	if maxMissingPercent < 0 || maxMissingPercent > 100 {
		return errors.New("--max-missing-percentage must be between 0 and 100")
	}
	return nil
}
