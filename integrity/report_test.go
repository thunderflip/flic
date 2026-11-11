package integrity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.csv")
	report := Report{FilePath: path, Separator: ';'}
	entries, err := report.Load()
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing report: %v, %v", entries, err)
	}
	data := "Last-Check;Mod-Time;File;Size\n2024-01-02 03:04:05.000000;1700000000.123456789;short.flac;42\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err = report.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].FilePath != "short.flac" || entries[0].FileSize != 42 || entries[0].FileModTime.Nanosecond() != 123456789 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if err := report.Save(entries); err != nil {
		t.Fatal(err)
	}
	again, err := report.Load()
	if err != nil || len(again) != 1 || again[0].FilePath != entries[0].FilePath || !again[0].FileModTime.Equal(entries[0].FileModTime) {
		t.Fatalf("round trip: %+v, %v", again, err)
	}
}

func TestInvalidReports(t *testing.T) {
	for _, data := range []string{"", "File;Size\na;1\n", "File;Size;Mod-Time;Last-Check\na;x;1.0;2024-01-01 00:00:00\n", "File;Size;Mod-Time;Last-Check\na;1;123;2024-01-01 00:00:00\n"} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			r := Report{FilePath: path, Separator: ';'}
			if _, err := r.Load(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFailedWritePreservesReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report")
	original := "original"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	r := Report{FilePath: path, Separator: ';'}
	if err := r.Save([]Entry{{FilePath: "missing date"}}); err == nil {
		t.Fatal("expected error")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("report changed: %q %v", content, err)
	}
}

func TestTimestampRoundTrip(t *testing.T) {
	for _, tm := range []time.Time{time.Unix(0, 0), time.Unix(-1, 123), time.Unix(1700000000, 123456789)} {
		got, err := parseUnixUTC(strUnixUTC(tm))
		if err != nil || !got.Equal(tm) {
			t.Fatalf("%v: %v %v", tm, got, err)
		}
	}
	if _, err := parseUnixUTC("1." + strings.Repeat("1", 10)); err == nil {
		t.Fatal("expected invalid precision")
	}
}

func TestReportUsesLocalCheckDates(t *testing.T) {
	previousLocation := time.Local
	time.Local = time.FixedZone("test", 2*60*60)
	t.Cleanup(func() { time.Local = previousLocation })
	checked := time.Date(2024, 7, 1, 12, 30, 0, 0, time.UTC)
	report := Report{FilePath: filepath.Join(t.TempDir(), "report.csv"), Separator: ';'}
	entries := []Entry{
		{FilePath: "checked.flac", FileModTime: checked, DateChecked: &checked},
		{FilePath: "new.flac", FileModTime: checked, DateChecked: &DateUndefined},
	}
	if err := report.Save(entries); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(report.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "2024-07-01 14:30:00") {
		t.Fatalf("expected local date in report: %s", content)
	}
	read, err := report.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !read[0].DateChecked.Equal(checked) || !read[1].DateChecked.Equal(DateUndefined) {
		t.Fatalf("unexpected check dates: %v, %v", read[0].DateChecked, read[1].DateChecked)
	}
}

func TestSaveLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	r := Report{FilePath: filepath.Join(dir, "report.csv"), Separator: ';'}
	for range 3 {
		if err := r.Save([]Entry{{FilePath: "a.flac", DateChecked: &DateUndefined}}); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "report.csv" {
		t.Fatalf("unexpected files: %v", files)
	}
}
