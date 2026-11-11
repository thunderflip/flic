package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thunderflip/flic/integrity"
)

func TestPathComparisonPreservesHistory(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "Album", "track.flac"))
	if err != nil {
		t.Fatal(err)
	}
	reportPath := path
	if runtime.GOOS == "windows" {
		reportPath = strings.ToUpper(path)
	}
	checked := time.Now().Add(-time.Hour)
	modified := time.Now()
	previous := []integrity.Entry{{FilePath: reportPath, FileSize: 123, FileModTime: modified, DateChecked: &checked}}
	files := []integrity.Entry{{FilePath: path, FileSize: 123, FileModTime: modified, DateChecked: &integrity.DateUndefined}}
	if err := checkMissingEntries(previous, files, 0); err != nil {
		t.Fatal(err)
	}
	result := mergeReportAndFiles(previous, files)
	if len(result) != 1 || !result[0].DateChecked.Equal(checked) || result[0].FilePath != files[0].FilePath {
		t.Fatalf("history or scanned path not preserved: %+v", result)
	}
}

func TestAmbiguousPathsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.flac")
	entries := []integrity.Entry{{FilePath: path}, {FilePath: path}}
	if err := validateUniquePaths(entries); err == nil {
		t.Fatal("expected duplicate path error")
	}
	if runtime.GOOS == "windows" {
		entries = []integrity.Entry{{FilePath: "Track.flac"}, {FilePath: "track.flac"}}
		if err := validateUniquePaths(entries); err == nil {
			t.Fatal("expected case collision error")
		}
	}
}

func TestMissingEntriesThreshold(t *testing.T) {
	previous := []integrity.Entry{{FilePath: "a"}, {FilePath: "b"}}
	for _, tc := range []struct {
		files     []integrity.Entry
		threshold int
		blocked   bool
	}{
		{previous, 0, false}, {previous[:1], 50, false}, {previous[:1], 49, true},
		{nil, 50, true}, {nil, 100, false},
	} {
		err := checkMissingEntries(previous, tc.files, tc.threshold)
		if (err != nil) != tc.blocked {
			t.Fatalf("threshold %d, files %v: %v", tc.threshold, tc.files, err)
		}
	}
	if err := checkMissingEntries(nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	for _, threshold := range []int{-1, 101} {
		opts := testOptions("flac", "folder", "report", 0)
		opts.age, opts.minPercent, opts.maxMissingPercent = given(0), optionalInt{}, threshold
		if err := validateOptions(opts); err == nil {
			t.Fatalf("accepted threshold %d", threshold)
		}
	}
}

func TestMissingEntriesPreserveReport(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	store := integrity.Report{FilePath: report, Separator: ';'}
	entries := []integrity.Entry{
		{FilePath: filepath.Join(dir, "a.flac"), DateChecked: &integrity.DateUndefined},
		{FilePath: filepath.Join(dir, "b.flac"), DateChecked: &integrity.DateUndefined},
	}
	if err := store.Save(entries); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(context.Background(), testOptions("flac", dir, report, 0)); err == nil {
		t.Fatal("expected missing files error")
	}
	after, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("report changed despite missing files")
	}
	if _, err := os.Stat(report + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock remains: %v", err)
	}
}

func TestSelectionLimit(t *testing.T) {
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, time.FixedZone("test", 3600))
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)
	entries := []integrity.Entry{{DateChecked: &old}, {DateChecked: &recent}, {DateChecked: &recent}}
	if got := selectionLimit(entries, options{age: given(-2)}, now); got != 1 {
		t.Fatalf("today: %d", got)
	}
	if got := selectionLimit(entries, options{minPercent: given(1)}, now); got != 1 {
		t.Fatalf("minimum: %d", got)
	}
	if got := selectionLimit(entries, options{age: given(0), maxPercent: given(33)}, now); got != 0 {
		t.Fatalf("maximum: %d", got)
	}
}

func TestSelectionLimitPerDay(t *testing.T) {
	now := time.Date(2024, 1, 3, 12, 0, 0, 0, time.FixedZone("test", 3600))
	yesterday, today := now.Add(-24*time.Hour), now.Add(-time.Hour)
	// 20 files sorted oldest first: 4 never checked, 10 checked yesterday, 6 today.
	var entries []integrity.Entry
	for i := 0; i < 20; i++ {
		date := &integrity.DateUndefined
		if i >= 4 {
			date = &yesterday
		}
		if i >= 14 {
			date = &today
		}
		entries = append(entries, integrity.Entry{DateChecked: date})
	}
	for _, tc := range []struct {
		name string
		opts options
		want int
	}{
		{"35% without --per-day", options{minPercent: given(35)}, 7},
		{"35% per day, 6 done", options{minPercent: given(35), perDay: true}, 1},
		{"30% per day, already reached", options{minPercent: given(30), perDay: true}, 0},
		{"25% per day, exceeded", options{minPercent: given(25), perDay: true}, 0},
		{"age keeps its own count", options{age: given(-1), minPercent: given(25), perDay: true}, 4},
		{"max 50% per day", options{age: given(-2), maxPercent: given(50), perDay: true}, 4},
		{"max 25% per day, exceeded", options{age: given(-2), maxPercent: given(25), perDay: true}, 0},
	} {
		if got := selectionLimit(entries, tc.opts, now); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestPerDayCompletesInterruptedRun(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d.flac", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FLIC_FAKE_FLAC", "ok")
	checkedToday := func() int {
		entries, err := (&integrity.Report{FilePath: report, Separator: ';'}).Load()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if !e.DateChecked.Before(startOfDay(time.Now())) {
				n++
			}
		}
		return n
	}
	run := func(minPercent int, perDay bool) {
		opts := testOptions(os.Args[0], dir, report, minPercent)
		opts.perDay = perDay
		if err := check(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
	}
	run(20, false) // a run stopped after 20% of the collection
	if got := checkedToday(); got != 2 {
		t.Fatalf("after the first run: %d checked today, want 2", got)
	}
	run(50, true) // completes the daily 50%
	if got := checkedToday(); got != 5 {
		t.Fatalf("after completing: %d checked today, want 5", got)
	}
	run(50, true) // target already reached: checks nothing
	if got := checkedToday(); got != 5 {
		t.Fatalf("after a further run: %d checked today, want 5", got)
	}
}

func TestFailedVerificationSavesProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.flac")
	report := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := check(context.Background(), testOptions(filepath.Join(dir, "missing-flac"), dir, report, 100)); err == nil {
		t.Fatal("expected verification failure")
	}
	store := integrity.Report{FilePath: report, Separator: ';'}
	entries, err := store.Load()
	if err != nil || len(entries) != 1 || !entries[0].DateChecked.Equal(integrity.DateUndefined) {
		t.Fatalf("saved progress: %+v %v", entries, err)
	}
}

func TestInterruptDuringScanLeavesReportUntouched(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := check(ctx, testOptions("flac", dir, report, 100))
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("expected interruption, got %v", err)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatalf("report was created: %v", err)
	}
	if _, err := os.Stat(report + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock remains: %v", err)
	}
}

func TestInterruptDuringVerificationSavesProgress(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(filepath.Join(dir, "a.flac"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLIC_FAKE_FLAC", "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := check(ctx, testOptions(os.Args[0], dir, report, 100))
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("expected interruption, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("flac was not stopped promptly: %s", elapsed)
	}
	store := integrity.Report{FilePath: report, Separator: ';'}
	entries, err := store.Load()
	if err != nil || len(entries) != 1 || !entries[0].DateChecked.Equal(integrity.DateUndefined) {
		t.Fatalf("saved progress: %+v %v", entries, err)
	}
	if _, err := os.Stat(report + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock remains: %v", err)
	}
}

// testOptions returns default options for a run on folder that checks at
// least minPercent of the collection.
func testOptions(flacPath, folder, report string, minPercent int) options {
	opts := defaultOptions()
	opts.flacPath, opts.folder, opts.report, opts.minPercent = flacPath, folder, report, given(minPercent)
	return opts
}
