package main

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thunderflip/flic-go/integrity"
)

// fakeFlacModes lets the test binary stand in for the flac executable: when
// FLIC_FAKE_FLAC names a mode, that mode runs instead of the tests.
// Platform-specific test files may register additional modes in init.
var fakeFlacModes = map[string]func(){
	"sleep": func() { time.Sleep(time.Minute) },
}

// TestMain applies to every test of package main.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FLIC_FAKE_FLAC"); mode != "" {
		fakeFlacModes[mode]()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInterruptDuringScanLeavesReportUntouched(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := check(ctx, "flac", dir, report, math.MinInt32, 100, math.MinInt32, 50)
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
	err := check(ctx, os.Args[0], dir, report, math.MinInt32, 100, math.MinInt32, 50)
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

func TestAnnounceInterruption(t *testing.T) {
	for _, tc := range []struct {
		name        string
		runFinished bool
		announced   bool
	}{
		{"signal during run", false, true},
		{"normal end of run", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			if tc.runFinished {
				close(done)
			}
			cancel()
			stopped := false
			var out strings.Builder
			announceInterruption(ctx, done, func() { stopped = true }, &out)
			if announced := out.Len() > 0; announced != tc.announced || stopped != tc.announced {
				t.Fatalf("announced %v, stopped %v, want %v: %q", announced, stopped, tc.announced, out.String())
			}
		})
	}
}
