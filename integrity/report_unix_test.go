//go:build unix

package integrity

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSaveKeepsReportPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0644, 0640, 0600} {
		path := filepath.Join(t.TempDir(), "report.csv")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		r := Report{FilePath: path, Separator: ';'}
		if err := r.Save([]Entry{{FilePath: "a.flac", DateChecked: &DateUndefined}}); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != mode {
			t.Fatalf("permissions %v after save, want %v", got, mode)
		}
	}
}

func TestNewReportFollowsUmask(t *testing.T) {
	umask := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(umask) })
	path := filepath.Join(t.TempDir(), "report.csv")
	r := Report{FilePath: path, Separator: ';'}
	if err := r.Save(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("new report permissions %v, want 0644", got)
	}
}
