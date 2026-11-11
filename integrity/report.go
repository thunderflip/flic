package integrity

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	columnFile      = "File"
	columnSize      = "Size"
	columnModTime   = "Mod-Time"
	columnLastCheck = "Last-Check"
	dateFormat      = "2006-01-02 15:04:05.999999"
)

// Report is the CSV file holding the check history.
type Report struct {
	FilePath  string
	Separator rune
}

// Load reads the report; a missing report yields no entries.
func (r *Report) Load() ([]Entry, error) {
	f, err := os.Open(r.FilePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open report %q: %w", r.FilePath, err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.Comma = r.Separator
	headers, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("report %q is empty", r.FilePath)
	}
	if err != nil {
		return nil, fmt.Errorf("read report header: %w", err)
	}
	indexes := make(map[string]int)
	for i, header := range headers {
		if _, exists := indexes[header]; exists {
			return nil, fmt.Errorf("duplicate report column %q", header)
		}
		indexes[header] = i
	}
	for _, header := range []string{columnFile, columnSize, columnModTime, columnLastCheck} {
		if _, ok := indexes[header]; !ok {
			return nil, fmt.Errorf("missing report column %q", header)
		}
	}
	// Extra columns cannot be preserved by the current report model.
	if len(headers) != 4 {
		return nil, fmt.Errorf("report contains unsupported columns")
	}
	var entries []Entry
	for line := 2; ; line++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read report row %d: %w", line, err)
		}
		size, err := strconv.ParseInt(row[indexes[columnSize]], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid file size on report row %d", line)
		}
		modified, err := parseUnixUTC(row[indexes[columnModTime]])
		if err != nil {
			return nil, fmt.Errorf("invalid modification time on report row %d: %w", line, err)
		}
		checkedText := row[indexes[columnLastCheck]]
		checked, err := time.ParseInLocation(dateFormat, checkedText, time.Local)
		if err != nil {
			return nil, fmt.Errorf("invalid check date on report row %d: %w", line, err)
		}
		// The sentinel is a fixed instant, independent of the machine's timezone.
		if checked.Format(dateFormat) == DateUndefined.Format(dateFormat) {
			checked = DateUndefined
		}
		entries = append(entries, Entry{FilePath: row[indexes[columnFile]], FileSize: size, FileModTime: modified, DateChecked: &checked})
	}
	return entries, nil
}

// Save atomically replaces the report with entries.
func (r *Report) Save(entries []Entry) (err error) {
	f, err := createTempFile(filepath.Dir(r.FilePath), ".flic-report-")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	name := f.Name()
	defer func() { f.Close(); os.Remove(name) }()
	// The temporary file replaces the report: keep the existing report's permissions.
	if info, statErr := os.Stat(r.FilePath); statErr == nil {
		if err = f.Chmod(info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy report permissions: %w", err)
		}
	}
	writer := csv.NewWriter(f)
	writer.Comma = r.Separator
	if err = writer.Write([]string{columnFile, columnSize, columnModTime, columnLastCheck}); err != nil {
		return fmt.Errorf("write report header: %w", err)
	}
	for _, entry := range entries {
		if entry.DateChecked == nil {
			return fmt.Errorf("missing check date for %q", entry.FilePath)
		}
		checked := entry.DateChecked.Local()
		if entry.DateChecked.Equal(DateUndefined) {
			checked = DateUndefined
		}
		row := []string{entry.FilePath, strconv.FormatInt(entry.FileSize, 10), strUnixUTC(entry.FileModTime), checked.Format(dateFormat)}
		if err = writer.Write(row); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	writer.Flush()
	if err = writer.Error(); err != nil {
		return fmt.Errorf("flush report: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync report: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err = os.Rename(name, r.FilePath); err != nil {
		return fmt.Errorf("replace report: %w", err)
	}
	return nil
}

// createTempFile is like os.CreateTemp, but the file is created with mode 0666
// filtered by the umask, as a regular new file would be, rather than 0600.
func createTempFile(dir, prefix string) (*os.File, error) {
	for range 100 {
		name := filepath.Join(dir, prefix+strconv.FormatUint(rand.Uint64(), 36))
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		if !errors.Is(err, os.ErrExist) {
			return f, err
		}
	}
	return nil, fmt.Errorf("no unused temporary name in %q", dir)
}

func parseUnixUTC(value string) (time.Time, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[1]) == 0 || len(parts[1]) > 9 {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
	}
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	fraction := parts[1] + strings.Repeat("0", 9-len(parts[1]))
	nanos, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || nanos < 0 {
		return time.Time{}, fmt.Errorf("invalid fractional seconds %q", parts[1])
	}
	return time.Unix(seconds, nanos).UTC(), nil
}
func strUnixUTC(value time.Time) string {
	return fmt.Sprintf("%d.%09d", value.Unix(), value.Nanosecond())
}
