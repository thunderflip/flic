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
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/thunderflip/flic/integrity"
)

const modtimeTolerance = time.Millisecond
const autosaveInterval = 3 * time.Minute
const defaultMaxMissingPercentage = 50
const optionNotSet = math.MinInt32

// exitInterrupted follows the shell convention for a run stopped by Ctrl+C (128 + SIGINT).
const exitInterrupted = 130

var errInterrupted = errors.New("interrupted")

// interruptSignals lists the signals that stop a run cleanly: Ctrl+C, termination
// requests (including closing the console window on Windows), and the hangup sent
// when a Unix terminal or SSH session closes. Signals that were ignored when flic
// started (for example SIGHUP under nohup) stay ignored.
func interruptSignals() []os.Signal {
	var signals []os.Signal
	for _, sig := range candidateInterruptSignals {
		if !signal.Ignored(sig) {
			signals = append(signals, sig)
		}
	}
	return signals
}

func mergeReportAndFiles(report, files []integrity.Entry) []integrity.Entry {
	old := make(map[string]integrity.Entry, len(report))
	for _, entry := range report {
		old[comparisonPath(entry.FilePath)] = entry
	}
	results := make([]integrity.Entry, 0, len(files))
	for _, file := range files {
		if entry, ok := old[comparisonPath(file.FilePath)]; ok && entry.FileSize == file.FileSize && entry.FileModTime.Sub(file.FileModTime).Abs() <= modtimeTolerance {
			entry.FilePath = file.FilePath
			entry.FileModTime = file.FileModTime
			file = entry
		}
		results = append(results, file)
	}
	return results
}

func comparisonPath(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func main() { os.Exit(run()) }

func run() int {
	flac := flag.String("flac", "", "Path to the flac executable")
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
	if err := validateOptions(*flac, *folder, *report, *age, *minPercent, *maxPercent, *maxMissingPercent); err != nil {
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
	if err := check(ctx, *flac, *folder, *report, *age, *minPercent, *maxPercent, *maxMissingPercent); err != nil {
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

func selectionLimit(entries []integrity.Entry, age, minPercent, maxPercent int, now time.Time) int {
	limit := 0
	if age != optionNotSet {
		var cutoff time.Time
		switch age {
		case -1:
			cutoff = integrity.DateUndefined
		case -2:
			y, m, d := now.Date()
			cutoff = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
		default:
			cutoff = now.Add(-time.Duration(age) * time.Minute)
		}
		limit = sort.Search(len(entries), func(i int) bool { return entries[i].DateChecked.After(cutoff) })
	}
	if minPercent != optionNotSet {
		n := int(math.Ceil(float64(len(entries)) * float64(minPercent) / 100))
		limit = max(limit, n)
	}
	if maxPercent != optionNotSet {
		n := int(math.Floor(float64(len(entries)) * float64(maxPercent) / 100))
		limit = min(limit, n)
	}
	return limit
}

func check(ctx context.Context, flac, folder, report string, age, minPercent, maxPercent, maxMissingPercent int) (resultErr error) {
	start := time.Now()
	lock, err := acquireReportLock(report)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.release()) }()
	store := integrity.Report{FilePath: report, Separator: ';'}
	previous, err := store.Load()
	if err != nil {
		return err
	}
	files, err := (integrity.Directory{DirPath: folder}).Scan(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w during scan; report was not modified", errInterrupted)
		}
		return err
	}
	if err := validateUniquePaths(previous); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := validateUniquePaths(files); err != nil {
		return fmt.Errorf("scanned files: %w", err)
	}
	if err := checkMissingEntries(previous, files, maxMissingPercent); err != nil {
		return err
	}
	entries := mergeReportAndFiles(previous, files)
	sort.Slice(entries, func(i, j int) bool { return entries[i].DateChecked.Before(*entries[j].DateChecked) })
	limit := selectionLimit(entries, age, minPercent, maxPercent, time.Now())
	if limit == len(entries) {
		sort.Slice(entries, func(i, j int) bool { return entries[i].FilePath < entries[j].FilePath })
	}
	fmt.Printf("Found %d FLAC file(s); checking %d.\n", len(entries), limit)
	lastSave := time.Now()
	checked := 0
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			return interrupted(store, entries, checked)
		}
		path := entries[i].FilePath
		fmt.Printf("Verifying (%d/%d): %s\n", i+1, limit, path)
		operation := FlacOperation{flacPath: flac, filePath: path}
		if err := operation.Test(ctx); err != nil {
			if ctx.Err() != nil {
				// flac was stopped by the interruption: the file was not verified.
				return interrupted(store, entries, checked)
			}
			saveErr := store.Save(entries)
			return errors.Join(fmt.Errorf("verify %q: %w", path, err), saveErr)
		}
		now := time.Now()
		entries[i].DateChecked = &now
		checked++
		if now.Sub(lastSave) >= autosaveInterval {
			if err := store.Save(entries); err != nil {
				return err
			}
			lastSave = now
		}
	}
	if err := store.Save(entries); err != nil {
		return err
	}
	fmt.Printf("Checked %d file(s) in %s.\n", checked, time.Since(start).Round(time.Millisecond))
	return nil
}

func interrupted(store integrity.Report, entries []integrity.Entry, checked int) error {
	if err := store.Save(entries); err != nil {
		return errors.Join(errInterrupted, err)
	}
	return fmt.Errorf("%w after checking %d file(s); progress saved", errInterrupted, checked)
}

func validateUniquePaths(entries []integrity.Entry) error {
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		key := comparisonPath(entry.FilePath)
		if first, ok := seen[key]; ok {
			return fmt.Errorf("ambiguous file paths %q and %q", first, entry.FilePath)
		}
		seen[key] = entry.FilePath
	}
	return nil
}

func checkMissingEntries(previous, files []integrity.Entry, maxPercent int) error {
	if len(previous) == 0 {
		return nil
	}
	present := make(map[string]struct{}, len(files))
	for _, entry := range files {
		present[comparisonPath(entry.FilePath)] = struct{}{}
	}
	missing := 0
	var examples []string
	for _, entry := range previous {
		if _, ok := present[comparisonPath(entry.FilePath)]; !ok {
			missing++
			if len(examples) < 3 {
				examples = append(examples, entry.FilePath)
			}
		}
	}
	percentage := float64(missing) * 100 / float64(len(previous))
	if percentage > float64(maxPercent) {
		return fmt.Errorf("%d of %d files from the report are missing (%.2f%%, maximum %d%%); report was not modified. Examples: %s", missing, len(previous), percentage, maxPercent, strings.Join(examples, ", "))
	}
	return nil
}
