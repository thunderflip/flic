package flac

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeFlacModes lets the test binary stand in for the flac executable: when
// FLIC_FAKE_FLAC names a mode, that mode runs instead of the tests.
// Platform-specific test files register modes in init.
var fakeFlacModes = map[string]func(){}

func TestMain(m *testing.M) {
	if mode := os.Getenv("FLIC_FAKE_FLAC"); mode != "" {
		fakeFlacModes[mode]()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestParseFlacHash(t *testing.T) {
	const hash = "84533df17b03dd13cbe3f2d2f1f26361"
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{hash + "\r\n", true}, {"0123456789ABCDEF0123456789ABCDEF", true},
		{hash[:31], false}, {hash + "0", false}, {"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", false}, {"", false},
	} {
		got, err := parseFlacHash(tc.input)
		if (err == nil) != tc.valid {
			t.Fatalf("input %q: hash %q, error %v", tc.input, got, err)
		}
		if tc.valid && len(got) != 32 {
			t.Fatalf("hash length = %d", len(got))
		}
	}
}

func TestGetHashIntegration(t *testing.T) {
	metaflac, file := os.Getenv("FLIC_TEST_METAFLAC"), os.Getenv("FLIC_TEST_FILE")
	if metaflac == "" || file == "" {
		t.Skip("set FLIC_TEST_METAFLAC and FLIC_TEST_FILE to run")
	}
	op := Operation{MetaflacPath: metaflac, FilePath: file}
	hash, err := op.GetHash()
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 32 {
		t.Fatalf("expected 32-character hash, got %q", hash)
	}
	if expected := os.Getenv("FLIC_TEST_HASH"); expected != "" && hash != expected {
		t.Fatalf("got hash %q, want %q", hash, expected)
	}
}

func TestFlacTestOutputOK(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		ok           bool
	}{
		{"success", "track.flac: ok\n", true},
		{"success with diagnostics", "flac version\ntrack.flac: ok\n", true},
		{"progress", "track.flac: testing, 87% complete\b\b\bok                    \r\n", true},
		{"missing md5", "track.flac: WARNING, cannot check MD5 signature since it was unset in the STREAMINFO\nok\n", false},
		{"missing streaminfo", "track.flac: WARNING, cannot check MD5 signature since there was no STREAMINFO\nok\n", false},
		{"ok in filename", "book.flac: no confirmation\n", false},
		{"ok followed by error", "track.flac: ok\nERROR\n", false},
		{"empty", "", false},
		{"missing confirmation", "track.flac: ERROR\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateFlacTestOutput(tc.output); (err == nil) != tc.ok {
				t.Fatalf("validateFlacTestOutput(%q) = %v, want success %v", tc.output, err, tc.ok)
			}
		})
	}
}

func TestFlacTestCommandFailure(t *testing.T) {
	op := Operation{FlacPath: filepath.Join(t.TempDir(), "missing-flac"), FilePath: "track.flac"}
	if err := op.Test(context.Background()); err == nil {
		t.Fatal("expected command failure")
	}
}
