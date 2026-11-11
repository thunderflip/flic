// Package flac runs the FLAC command-line tools.
package flac

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
)

var (
	md5Pattern      = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	verifyOKPattern = regexp.MustCompile(`Verify OK,`)
	testOKPattern   = regexp.MustCompile(`(?:^|:|\x08|\r)\s*ok\s*$`)
)

// Operation runs the flac and metaflac tools on one file.
type Operation struct {
	FlacPath     string // path to the flac executable
	MetaflacPath string // path to the metaflac executable
	FilePath     string // FLAC file to process
}

// GetHash returns the MD5 signature stored in the file, using metaflac.
func (f *Operation) GetHash() (string, error) {
	arg := []string{"--show-md5sum", f.FilePath}
	output, err := exec.Command(f.MetaflacPath, arg...).Output()
	if err != nil {
		log.Printf("Error running metaflac: %v", err)
		return "", err
	}

	hash, err := parseFlacHash(string(output))
	if err != nil {
		log.Printf("Failed to parse metaflac output: %s", string(output))
		return "", err
	}

	return hash, nil
}

func parseFlacHash(output string) (string, error) {
	hash := strings.TrimSpace(output)
	if !md5Pattern.MatchString(hash) {
		return "", fmt.Errorf("invalid metaflac MD5 output")
	}
	return hash, nil
}

// Reencode re-encodes the file in place with flac and verifies the result.
func (f *Operation) Reencode() (bool, error) {
	args := []string{"--force", "--no-error-on-compression-fail", "--verify", f.FilePath}
	out, err := exec.Command(f.FlacPath, args...).CombinedOutput()
	if err != nil {
		log.Printf("Error running flac: %v", err)
		return false, err
	}

	output := string(out)
	if strings.TrimSpace(output) == "" {
		log.Println("FLAC output expected")
		return false, fmt.Errorf("FLAC output is empty")
	}

	// Search for 'Verify OK' pattern
	matched := verifyOKPattern.MatchString(output)
	if !matched {
		log.Printf("FLAC verification failed:\n%s", output)
		return false, fmt.Errorf("FLAC verification failed")
	}

	log.Println("FLAC reencode successful")
	return true, nil
}

// Test decodes the file with flac and checks its stored MD5 signature.
func (f *Operation) Test(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, f.FlacPath, "--test", f.FilePath)
	// flac must not receive the user's Ctrl+C itself: otherwise it may die before
	// flic notices the interruption, and the run would be reported as a failed
	// verification. flic stops flac through ctx instead.
	isolateFromTerminalSignals(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run flac: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return validateFlacTestOutput(string(output))
}

func validateFlacTestOutput(output string) error {
	if strings.Contains(output, "WARNING, cannot check MD5 signature") {
		return fmt.Errorf("FLAC MD5 verification unavailable: %s", strings.TrimSpace(output))
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !testOKPattern.MatchString(last) {
		return fmt.Errorf("FLAC verification failed: expected final ok in output: %s", strings.TrimSpace(output))
	}
	return nil
}
