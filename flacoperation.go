package main

import (
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

type FlacOperation struct{ flacPath, metaflacPath, filePath string }

func (f *FlacOperation) GetHash() (string, error) {
	arg := []string{"--show-md5sum", f.filePath}
	output, err := exec.Command(f.metaflacPath, arg...).Output()
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

func (f *FlacOperation) Reencode() (bool, error) {
	args := []string{"--force", "--no-error-on-compression-fail", "--verify", f.filePath}
	out, err := exec.Command(f.flacPath, args...).CombinedOutput()
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

func (f *FlacOperation) Test() error {
	output, err := exec.Command(f.flacPath, "--test", f.filePath).CombinedOutput()
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
