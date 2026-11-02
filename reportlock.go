package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type reportLock struct {
	path string
	info os.FileInfo
}

func acquireReportLock(report string) (*reportLock, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("identify machine for report lock: %w", err)
	}
	path := report + ".lock"
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		message := fmt.Sprintf("report is locked: %q", path)
		if existing, openErr := os.Open(path); openErr == nil {
			data, readErr := io.ReadAll(io.LimitReader(existing, 4096))
			existing.Close()
			if readErr == nil && len(strings.TrimSpace(string(data))) > 0 {
				message += "\n" + strings.TrimSpace(string(data))
			}
		}
		return nil, errors.New(message + "\nIf no process is using this report, remove the lock file manually.")
	}
	if err != nil {
		return nil, fmt.Errorf("create report lock %q: %w", path, err)
	}
	_, writeErr := fmt.Fprintf(f, "Machine: %s\nPID: %d\nStarted: %s\n", host, os.Getpid(), time.Now().Format(time.RFC3339))
	info, statErr := f.Stat()
	closeErr := f.Close()
	if err := errors.Join(writeErr, statErr, closeErr); err != nil {
		removeErr := os.Remove(path)
		return nil, errors.Join(fmt.Errorf("write report lock %q: %w", path, err), removeErr)
	}
	return &reportLock{path: path, info: info}, nil
}

func (lock *reportLock) release() error {
	info, err := os.Stat(lock.path)
	if err != nil {
		return fmt.Errorf("inspect report lock %q before release: %w", lock.path, err)
	}
	if !os.SameFile(lock.info, info) {
		return fmt.Errorf("report lock %q has changed; it was not removed", lock.path)
	}
	if err := os.Remove(lock.path); err != nil {
		return fmt.Errorf("remove report lock %q: %w", lock.path, err)
	}
	return nil
}
