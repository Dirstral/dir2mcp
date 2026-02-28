package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dir2mcp/internal/config"
)

// EnsureStateDir creates .dir2mcp/ and subdirs, writes secret.token if missing,
// writes config snapshot, and acquires locks/index.lock (SPEC §4.1, hackathon Task 1.19).
func EnsureStateDir(stateDir string, cfg *config.Config) error {
	dirs := []string{
		stateDir,
		filepath.Join(stateDir, "cache", "ocr"),
		filepath.Join(stateDir, "cache", "transcribe"),
		filepath.Join(stateDir, "cache", "annotations"),
		filepath.Join(stateDir, "payments"),
		filepath.Join(stateDir, "locks"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create state dir %s: %w", d, err)
		}
	}

	lockPath := filepath.Join(stateDir, "locks", "index.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("another dir2mcp up is already running (lock: %s)", lockPath)
		}
		return err
	}
	_ = lockFile.Close()
	// Lock is advisory; remove on failure so future runs can proceed.
	removeLockOnError := true
	defer func() {
		if removeLockOnError {
			_ = os.Remove(lockPath)
		}
	}()

	tokenPath := filepath.Join(stateDir, "secret.token")
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		token, err := generateToken()
		if err != nil {
			return err
		}
		if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0600); err != nil {
			return fmt.Errorf("write secret.token: %w", err)
		}
	}

	removeLockOnError = false // Success; caller holds lock until shutdown
	return config.WriteSnapshot(stateDir, cfg)
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ResolveAuthToken returns the bearer token and a label for where it came from.
// authMode: "auto" | "none" | "file:<path>". For "auto", we read stateDir/secret.token.
func ResolveAuthToken(stateDir, authMode string) (token, source string, err error) {
	if authMode == "none" {
		return "", "none", nil
	}
	if strings.HasPrefix(authMode, "file:") {
		path := strings.TrimPrefix(authMode, "file:")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", fmt.Errorf("read auth file %s: %w", path, err)
		}
		tok := strings.TrimSpace(string(data))
		return tok, "file", nil
	}
	// auto or default: read from state dir
	tokenPath := filepath.Join(stateDir, "secret.token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", "", fmt.Errorf("read secret.token: %w", err)
	}
	return strings.TrimSpace(string(data)), "secret.token", nil
}
