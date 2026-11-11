package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thunderflip/flic/flac"
	"github.com/thunderflip/flic/integrity"
)

const (
	modtimeTolerance            = time.Millisecond
	autosaveInterval            = 3 * time.Minute
	defaultMaxMissingPercentage = 50
)

var errInterrupted = errors.New("interrupted")

// options holds the settings of a run.
type options struct {
	flacPath, folder, report string
	// age, minPercent and maxPercent select the files to check.
	age, minPercent, maxPercent optionalInt
	// perDay makes minPercent and maxPercent daily totals: files already
	// checked today count toward them.
	perDay            bool
	maxMissingPercent int
}

// defaultOptions returns options with no file selection and the default
// limit on missing files.
func defaultOptions() options {
	return options{maxMissingPercent: defaultMaxMissingPercentage}
}

// optionalInt is an integer option that remembers whether it was given; its
// zero value means "not given". It implements flag.Value, and since its zero
// value prints as "", the help shows no default for it.
type optionalInt struct {
	value int
	set   bool
}

// given returns an optionalInt that was given with value v.
func given(v int) optionalInt { return optionalInt{value: v, set: true} }

func (o *optionalInt) String() string {
	if o == nil || !o.set {
		return ""
	}
	return strconv.Itoa(o.value)
}

func (o *optionalInt) Set(s string) error {
	v, err := strconv.ParseInt(s, 0, strconv.IntSize)
	if err != nil {
		return errors.New("parse error")
	}
	*o = given(int(v))
	return nil
}

// check runs one verification pass: it locks the report, merges it with the
// FLAC files found under opts.folder, verifies the selected files with flac,
// oldest check first, and saves the report periodically, at the end, and when
// a verification fails or the run is interrupted.
func check(ctx context.Context, opts options) (resultErr error) {
	start := time.Now()
	lock, err := acquireReportLock(opts.report)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.release()) }()
	store := integrity.Report{FilePath: opts.report, Separator: ';'}
	previous, err := store.Load()
	if err != nil {
		return err
	}
	files, err := (integrity.Directory{DirPath: opts.folder}).Scan(ctx)
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
	if err := checkMissingEntries(previous, files, opts.maxMissingPercent); err != nil {
		return err
	}
	entries := mergeReportAndFiles(previous, files)
	sort.Slice(entries, func(i, j int) bool { return entries[i].DateChecked.Before(*entries[j].DateChecked) })
	now := time.Now()
	limit := selectionLimit(entries, opts, now)
	if opts.perDay {
		fmt.Printf("Found %d FLAC file(s), %d already checked today; checking %d.\n", len(entries), checkedSince(entries, startOfDay(now)), limit)
	} else {
		fmt.Printf("Found %d FLAC file(s); checking %d.\n", len(entries), limit)
	}
	if limit == len(entries) {
		sort.Slice(entries, func(i, j int) bool { return entries[i].FilePath < entries[j].FilePath })
	}
	lastSave := time.Now()
	checked := 0
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			return interrupted(store, entries, checked)
		}
		path := entries[i].FilePath
		fmt.Printf("Verifying (%d/%d): %s\n", i+1, limit, path)
		operation := flac.Operation{FlacPath: opts.flacPath, FilePath: path}
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

// selectionLimit returns how many entries to check, taking them from the
// start of entries, which must be sorted by check date, oldest first.
func selectionLimit(entries []integrity.Entry, opts options, now time.Time) int {
	limit := 0
	if opts.age.set {
		var cutoff time.Time
		switch opts.age.value {
		case -1:
			cutoff = integrity.DateUndefined
		case -2:
			cutoff = startOfDay(now)
		default:
			cutoff = now.Add(-time.Duration(opts.age.value) * time.Minute)
		}
		limit = len(entries) - checkedSince(entries, cutoff)
	}
	// With --per-day, files already checked today count toward the percentages.
	checkedToday := 0
	if opts.perDay {
		checkedToday = checkedSince(entries, startOfDay(now))
	}
	if opts.minPercent.set {
		n := int(math.Ceil(float64(len(entries))*float64(opts.minPercent.value)/100)) - checkedToday
		limit = max(limit, n)
	}
	if opts.maxPercent.set {
		n := int(math.Floor(float64(len(entries))*float64(opts.maxPercent.value)/100)) - checkedToday
		limit = max(min(limit, n), 0)
	}
	return limit
}

// checkedSince returns how many entries were last checked after t; entries
// must be sorted by check date, oldest first.
func checkedSince(entries []integrity.Entry, t time.Time) int {
	return len(entries) - sort.Search(len(entries), func(i int) bool { return entries[i].DateChecked.After(t) })
}

// startOfDay returns midnight at the start of t's day, in t's time zone.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
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

func interrupted(store integrity.Report, entries []integrity.Entry, checked int) error {
	if err := store.Save(entries); err != nil {
		return errors.Join(errInterrupted, err)
	}
	return fmt.Errorf("%w after checking %d file(s); progress saved", errInterrupted, checked)
}
