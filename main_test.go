package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thunderflip/flic-go/integrity"
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

func TestValidateOptions(t *testing.T) {
	unset := math.MinInt32
	for _, tc := range []struct {
		age, min, max int
		valid         bool
	}{{unset, unset, unset, false}, {unset, unset, 10, false}, {0, unset, 10, true}, {unset, 1, unset, true}, {-3, unset, unset, false}, {0, 101, unset, false}, {0, 0, unset, true}, {0, 1, 1, false}} {
		err := validateOptions("flac", "folder", "report", tc.age, tc.min, tc.max, 50)
		if (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
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
		if err := validateOptions("flac", "folder", "report", 0, math.MinInt32, math.MinInt32, threshold); err == nil {
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
	if err := check("flac", dir, report, math.MinInt32, 0, math.MinInt32, 50); err == nil {
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
	if got := selectionLimit(entries, -2, math.MinInt32, math.MinInt32, now); got != 1 {
		t.Fatalf("today: %d", got)
	}
	if got := selectionLimit(entries, math.MinInt32, 1, math.MinInt32, now); got != 1 {
		t.Fatalf("minimum: %d", got)
	}
	if got := selectionLimit(entries, 0, math.MinInt32, 33, now); got != 0 {
		t.Fatalf("maximum: %d", got)
	}
}
func TestFailedVerificationSavesProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.flac")
	report := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := check(filepath.Join(dir, "missing-flac"), dir, report, math.MinInt32, 100, math.MinInt32, 50); err == nil {
		t.Fatal("expected verification failure")
	}
	store := integrity.Report{FilePath: report, Separator: ';'}
	entries, err := store.Load()
	if err != nil || len(entries) != 1 || !entries[0].DateChecked.Equal(integrity.DateUndefined) {
		t.Fatalf("saved progress: %+v %v", entries, err)
	}
}
