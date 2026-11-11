package main

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain applies to every test of package main. With FLIC_FAKE_FLAC=sleep,
// the test binary stands in for a slow flac executable instead of running tests.
func TestMain(m *testing.M) {
	if os.Getenv("FLIC_FAKE_FLAC") == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
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
