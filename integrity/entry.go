package integrity

import "time"

// DateUndefined marks files that have never been checked.
var DateUndefined = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

// Entry describes a FLAC file and the date of its last successful check.
type Entry struct {
	FilePath    string
	FileSize    int64
	FileModTime time.Time
	DateChecked *time.Time
}
