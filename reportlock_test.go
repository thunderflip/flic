package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestReportLockExcludesOtherChecks(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	lock, err := acquireReportLock(report)
	if err != nil {
		t.Fatal(err)
	}
	err = check("flac", dir, report, math.MinInt32, 100, math.MinInt32, 50)
	if err == nil || !strings.Contains(err.Error(), "PID:") || !strings.Contains(err.Error(), "Machine:") {
		t.Fatalf("expected lock details, got %v", err)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatalf("report was created while locked: %v", err)
	}
	if err := lock.release(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireReportLock(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.release(); err != nil {
		t.Fatal(err)
	}
}

func TestReportLockConcurrentAcquisition(t *testing.T) {
	report := filepath.Join(t.TempDir(), "report.csv")
	const count = 10
	locks := make(chan *reportLock, count)
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if lock, err := acquireReportLock(report); err == nil {
				locks <- lock
			}
		}()
	}
	group.Wait()
	close(locks)
	if len(locks) != 1 {
		t.Fatalf("expected one lock owner, got %d", len(locks))
	}
	if err := (<-locks).release(); err != nil {
		t.Fatal(err)
	}
}

func TestReportLockReleasedAfterFailure(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.csv")
	if err := check("flac", filepath.Join(dir, "missing"), report, math.MinInt32, 100, math.MinInt32, 50); err == nil {
		t.Fatal("expected directory error")
	}
	if _, err := os.Stat(report + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock remains: %v", err)
	}
}
