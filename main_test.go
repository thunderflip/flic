package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain applies to every test of package main. With FLIC_FAKE_FLAC set, the
// test binary stands in for the flac executable instead of running tests:
// "sleep" behaves like a slow flac, "ok" like a successful verification.
func TestMain(m *testing.M) {
	switch os.Getenv("FLIC_FAKE_FLAC") {
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "ok":
		fmt.Println("track.flac: ok")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestValidateOptions(t *testing.T) {
	unset := optionalInt{}
	for _, tc := range []struct {
		age, min, max optionalInt
		valid         bool
	}{
		{unset, unset, unset, false}, {unset, unset, given(10), false}, {given(0), unset, given(10), true},
		{unset, given(1), unset, true}, {given(-3), unset, unset, false}, {given(0), given(101), unset, false},
		{given(0), given(0), unset, true}, {given(0), given(1), given(1), false},
		// The former "not set" marker is now an ordinary, invalid age.
		{given(math.MinInt32), unset, unset, false},
	} {
		opts := defaultOptions()
		opts.flacPath, opts.folder, opts.report = "flac", "folder", "report"
		opts.age, opts.minPercent, opts.maxPercent = tc.age, tc.min, tc.max
		err := validateOptions(opts)
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

func TestFlags(t *testing.T) {
	parse := func(args ...string) (options, error) {
		opts := defaultOptions()
		fs := flag.NewFlagSet("flic", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		defineFlags(fs, &opts)
		return opts, fs.Parse(args)
	}
	opts, err := parse("--flac=f", "--folder=d", "--report=r", "--age=-2", "--max-percentage", "25", "--per-day")
	if err != nil {
		t.Fatal(err)
	}
	want := options{flacPath: "f", folder: "d", report: "r", age: given(-2), maxPercent: given(25), perDay: true, maxMissingPercent: defaultMaxMissingPercentage}
	if opts != want {
		t.Fatalf("parsed %+v, want %+v", opts, want)
	}
	if opts, err := parse("--age=0"); err != nil || opts.age != given(0) || opts.minPercent.set {
		t.Fatalf("--age=0 must be given and distinct from unset: %+v %v", opts, err)
	}
	if _, err := parse("--age=x"); err == nil {
		t.Fatal("accepted a non-numeric age")
	}
}

func TestHelpShowsNoInternalDefaults(t *testing.T) {
	opts := defaultOptions()
	fs := flag.NewFlagSet("flic", flag.ContinueOnError)
	var help strings.Builder
	fs.SetOutput(&help)
	defineFlags(fs, &opts)
	fs.Usage()
	text := help.String()
	for _, want := range []string{"Usage: flic", "-age minutes", "-min-percentage percentage", "(default 50)"} {
		if !strings.Contains(text, want) {
			t.Errorf("help lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "(default") != 1 || strings.Contains(text, "-2147483648") {
		t.Errorf("help shows unexpected defaults:\n%s", text)
	}
}

func TestPerDayRequiresPercentage(t *testing.T) {
	opts := defaultOptions()
	opts.flacPath, opts.folder, opts.report = "flac", "folder", "report"
	opts.age, opts.perDay = given(-1), true
	if err := validateOptions(opts); err == nil {
		t.Fatal("accepted --per-day without a percentage")
	}
	for _, set := range []func(*options){
		func(o *options) { o.minPercent = given(35) },
		func(o *options) { o.maxPercent = given(35) },
	} {
		o := opts
		set(&o)
		if err := validateOptions(o); err != nil {
			t.Fatal(err)
		}
	}
}
