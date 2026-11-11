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
	opts := defaultOptions()
	defineFlags(flag.CommandLine, &opts)
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected argument %q; options must use the --name=value form\n", flag.Arg(0))
		return 2
	}
	if err := validateOptions(opts); err != nil {
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
	if err := check(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if errors.Is(err, errInterrupted) {
			return exitInterrupted
		}
		return 1
	}
	return 0
}

// defineFlags declares the command-line options of fs, stored into opts, and
// its help message. The current values of opts are the defaults.
func defineFlags(fs *flag.FlagSet, opts *options) {
	fs.StringVar(&opts.flacPath, "flac", opts.flacPath, "`path` to the flac executable")
	fs.StringVar(&opts.folder, "folder", opts.folder, "root `folder` of the FLAC collection")
	fs.StringVar(&opts.report, "report", opts.report, "`path` to the report file")
	fs.Var(&opts.age, "age", "select files by the age of their last check, in `minutes` (-1: never checked, -2: before today)")
	fs.Var(&opts.minPercent, "min-percentage", "minimum `percentage` of the collection to check")
	fs.Var(&opts.maxPercent, "max-percentage", "maximum `percentage` of the collection to check (with --age)")
	fs.IntVar(&opts.maxMissingPercent, "max-missing-percentage", opts.maxMissingPercent, "maximum `percentage` of report entries that may be missing")
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage: flic --flac=PATH --folder=FOLDER --report=PATH (--age=MINUTES | --min-percentage=PERCENT) [options]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Options:")
		fs.PrintDefaults()
	}
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

func validateOptions(opts options) error {
	if opts.flacPath == "" || opts.folder == "" || opts.report == "" {
		return errors.New("--flac, --folder and --report are required")
	}
	if opts.minPercent.set && opts.maxPercent.set {
		return errors.New("--min-percentage and --max-percentage cannot be combined")
	}
	if !opts.age.set && !opts.minPercent.set {
		return errors.New("specify --age or --min-percentage; --max-percentage requires --age")
	}
	if age := opts.age.value; opts.age.set && (age < -2 || int64(age) > int64(math.MaxInt64)/int64(time.Minute)) {
		return errors.New("--age must be -2, -1, or a non-negative number of minutes within the supported duration")
	}
	for _, p := range []optionalInt{opts.minPercent, opts.maxPercent} {
		if p.set && (p.value < 0 || p.value > 100) {
			return errors.New("percentages must be between 0 and 100")
		}
	}
	if opts.maxMissingPercent < 0 || opts.maxMissingPercent > 100 {
		return errors.New("--max-missing-percentage must be between 0 and 100")
	}
	return nil
}
