package integrity

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Directory is a FLAC collection scanned recursively.
type Directory struct{ DirPath string }

// Scan lists the regular .flac files under DirPath; new entries are never checked.
func (dir Directory) Scan(ctx context.Context) ([]Entry, error) {
	var results []Entry
	err := filepath.WalkDir(dir.DirPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".flac") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("get file info %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		results = append(results, Entry{FilePath: path, FileSize: info.Size(), FileModTime: info.ModTime(), DateChecked: &DateUndefined})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan directory %q: %w", dir.DirPath, err)
	}
	return results, nil
}
