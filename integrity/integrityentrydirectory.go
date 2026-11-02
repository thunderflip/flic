package integrity

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

type IntegrityEntryDirectory struct{ DirPath string }

func (ied IntegrityEntryDirectory) GetIntegrityEntries() ([]IntegrityEntry, error) {
	var results []IntegrityEntry
	err := filepath.WalkDir(ied.DirPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
		results = append(results, IntegrityEntry{FilePath: path, FileSize: info.Size(), FileModTime: info.ModTime(), DateChecked: &DATE_UNDEFINED})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan directory %q: %w", ied.DirPath, err)
	}
	return results, nil
}
