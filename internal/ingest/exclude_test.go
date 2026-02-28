package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"dir2mcp/internal/config"
)

func TestMatchesAnyPathExclude(t *testing.T) {
	patterns := []string{
		"**/private/**",
		"**/*.pem",
		"fixtures/",
	}

	if !matchesAnyPathExclude("src/private/token.txt", patterns) {
		t.Fatal("expected private path to match")
	}
	if !matchesAnyPathExclude("tls/server.pem", patterns) {
		t.Fatal("expected pem path to match")
	}
	if !matchesAnyPathExclude("fixtures/data/sample.json", patterns) {
		t.Fatal("expected fixtures/ prefix path to match")
	}
	if matchesAnyPathExclude("src/public/readme.md", patterns) {
		t.Fatal("did not expect public path to match")
	}
}

func TestCompileSecretPatterns(t *testing.T) {
	if _, err := compileSecretPatterns([]string{"["}); err == nil {
		t.Fatal("expected invalid regexp compile error")
	}

	patterns, err := compileSecretPatterns(config.Default().SecretPatterns)
	if err != nil {
		t.Fatalf("compile defaults failed: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("expected compiled default patterns")
	}
}

func TestDetectSecretInFile(t *testing.T) {
	root := t.TempDir()
	secretFile := filepath.Join(root, "secret.txt")
	safeFile := filepath.Join(root, "safe.txt")

	secretText := []byte("Authorization: Bearer abcdefgh.ijklmnop.qrstuvwx\n")
	safeText := []byte("hello world\n")
	if err := os.WriteFile(secretFile, secretText, 0o600); err != nil {
		t.Fatalf("WriteFile secret: %v", err)
	}
	if err := os.WriteFile(safeFile, safeText, 0o600); err != nil {
		t.Fatalf("WriteFile safe: %v", err)
	}

	patterns, err := compileSecretPatterns(config.Default().SecretPatterns)
	if err != nil {
		t.Fatalf("compile defaults failed: %v", err)
	}

	isSecret, err := detectSecretInFile(secretFile, patterns)
	if err != nil {
		t.Fatalf("detectSecretInFile secret failed: %v", err)
	}
	if !isSecret {
		t.Fatal("expected secret file to match")
	}

	isSecret, err = detectSecretInFile(safeFile, patterns)
	if err != nil {
		t.Fatalf("detectSecretInFile safe failed: %v", err)
	}
	if isSecret {
		t.Fatal("expected safe file to not match")
	}
}
