package integrity

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	FILE_PATH    = "File"
	FILE_SIZE    = "Size"
	FILE_MODTIME = "Mod-Time"
	DATE_CHECKED = "Last-Check"
	DATE_FORMAT  = "2006-01-02 15:04:05.999999"
)

type IntegrityEntryReport struct {
	FilePath  string
	Separator rune
}

func (ier *IntegrityEntryReport) GetIntegrityEntries() ([]IntegrityEntry, error) {
	f, err := os.Open(ier.FilePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open report %q: %w", ier.FilePath, err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.Comma = ier.Separator
	headers, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("report %q is empty", ier.FilePath)
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
	for _, header := range []string{FILE_PATH, FILE_SIZE, FILE_MODTIME, DATE_CHECKED} {
		if _, ok := indexes[header]; !ok {
			return nil, fmt.Errorf("missing report column %q", header)
		}
	}
	// Extra columns cannot be preserved by the current report model.
	if len(headers) != 4 {
		return nil, fmt.Errorf("report contains unsupported columns")
	}
	var entries []IntegrityEntry
	for line := 2; ; line++ {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read report row %d: %w", line, err)
		}
		size, err := strconv.ParseInt(row[indexes[FILE_SIZE]], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid file size on report row %d", line)
		}
		modified, err := parseUnixUTC(row[indexes[FILE_MODTIME]])
		if err != nil {
			return nil, fmt.Errorf("invalid modification time on report row %d: %w", line, err)
		}
		checkedText := row[indexes[DATE_CHECKED]]
		checked, err := time.ParseInLocation(DATE_FORMAT, checkedText, time.Local)
		if err != nil {
			return nil, fmt.Errorf("invalid check date on report row %d: %w", line, err)
		}
		// The sentinel is a fixed instant, independent of the machine's timezone.
		if checked.Format(DATE_FORMAT) == DATE_UNDEFINED.Format(DATE_FORMAT) {
			checked = DATE_UNDEFINED
		}
		entries = append(entries, IntegrityEntry{FilePath: row[indexes[FILE_PATH]], FileSize: size, FileModTime: modified, DateChecked: &checked})
	}
	return entries, nil
}

func (ier *IntegrityEntryReport) SetIntegrityEntries(entries []IntegrityEntry) (err error) {
	f, err := os.CreateTemp(filepath.Dir(ier.FilePath), ".flic-report-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	name := f.Name()
	defer func() { f.Close(); os.Remove(name) }()
	writer := csv.NewWriter(f)
	writer.Comma = ier.Separator
	if err = writer.Write([]string{FILE_PATH, FILE_SIZE, FILE_MODTIME, DATE_CHECKED}); err != nil {
		return fmt.Errorf("write report header: %w", err)
	}
	for _, entry := range entries {
		if entry.DateChecked == nil {
			return fmt.Errorf("missing check date for %q", entry.FilePath)
		}
		checked := entry.DateChecked.Local()
		if entry.DateChecked.Equal(DATE_UNDEFINED) {
			checked = DATE_UNDEFINED
		}
		row := []string{entry.FilePath, strconv.FormatInt(entry.FileSize, 10), strUnixUTC(entry.FileModTime), checked.Format(DATE_FORMAT)}
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
	if err = os.Rename(name, ier.FilePath); err != nil {
		return fmt.Errorf("replace report: %w", err)
	}
	return nil
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
