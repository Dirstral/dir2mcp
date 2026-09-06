package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dirstral/dir2mcp/internal/config"
	"github.com/dirstral/dir2mcp/internal/provider"
)

// TestLateChunking_ContextualIsMutuallyExclusive pins SPEC §8.1.9:
// ingest.late_chunking together with retrieval.contextual.enabled is
// CONFIG_INVALID (late chunking pools the document's own tokens, so a generated
// context has nowhere to go), while each alone validates.
func TestLateChunking_ContextualIsMutuallyExclusive(t *testing.T) {
	both := config.Default()
	both.IngestLateChunking = true
	both.RetrievalContextualEnabled = true
	err := both.Validate()
	if err == nil {
		t.Fatal("late_chunking + contextual must be CONFIG_INVALID")
	}
	if !strings.Contains(err.Error(), "CONFIG_INVALID") || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error must be a CONFIG_INVALID naming the exclusion: %v", err)
	}

	onlyLate := config.Default()
	onlyLate.IngestLateChunking = true
	if err := onlyLate.Validate(); err != nil {
		t.Fatalf("late_chunking alone must validate: %v", err)
	}
	onlyCtx := config.Default()
	onlyCtx.RetrievalContextualEnabled = true
	if err := onlyCtx.Validate(); err != nil {
		t.Fatalf("contextual alone must validate: %v", err)
	}
}

// TestProviders_TEISelfHosted pins dir2mcp#565 / SPEC §8.1.1: the built-in tei
// profile (a) takes its base_url from ${TEI_BASE_URL}, (b) is credential-less,
// (c) resolves for embed via an explicit binding, and (d) records its endpoint
// in the embed identity (no shipped default, so the endpoint is never
// normalized away) with the served model named by embed_text_model.
func TestProviders_TEISelfHosted(t *testing.T) {
	blankBuiltinProviderCreds(t)
	t.Setenv("TEI_BASE_URL", "http://gpu-box:8080")

	yaml := "version: 1\n" +
		"ingest:\n" +
		"  late_chunking: true\n" +
		"model:\n" +
		"  embed:\n" +
		"    provider: tei\n" +
		"    text_model: sentence-transformers/all-MiniLM-L6-v2\n" +
		"    code_model: sentence-transformers/all-MiniLM-L6-v2\n"
	cfg := loadCfg(t, yaml)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	r := cfg.Providers()
	p, err := r.Resolve(provider.CapEmbed)
	if err != nil {
		t.Fatalf("resolve tei embed: %v", err)
	}
	if p.Kind != provider.KindTEI {
		t.Fatalf("kind = %q, want tei", p.Kind)
	}
	if p.BaseURL != "http://gpu-box:8080" {
		t.Fatalf("base_url = %q, want ${TEI_BASE_URL} expansion", p.BaseURL)
	}
	if !p.CredentialLess {
		t.Fatal("tei builtin must be credential-less (no api_key)")
	}
	id := r.EmbedIdentity()
	if !strings.HasPrefix(id, "tei|http://gpu-box:8080|sentence-transformers/all-MiniLM-L6-v2|") {
		t.Fatalf("identity %q must record the endpoint (no default to normalize away) and the served model", id)
	}
	if !strings.HasSuffix(id, "|on|off") {
		t.Fatalf("identity %q must carry late_chunking=on", id)
	}
}

// TestProviders_TEINotInAutoPrecedence pins that the credential-less tei
// profile never wins embed auto-selection: with no real credentials and no
// explicit binding, embed must fail rather than silently pick a self-hosted box.
func TestProviders_TEINotInAutoPrecedence(t *testing.T) {
	blankBuiltinProviderCreds(t)
	t.Setenv("TEI_BASE_URL", "http://gpu-box:8080")
	cfg := loadCfg(t, "version: 1\n")
	if _, err := cfg.Providers().Resolve(provider.CapEmbed); err == nil {
		t.Fatal("tei must not win embed auto-selection (excluded from precedence)")
	}
}

// TestProviders_TEIDefaultModelLabel pins that a tei profile with no model
// records the "tei" label rather than an empty model component, so the identity
// fence (SPEC 8.1.4) is never inert for this kind.
func TestProviders_TEIDefaultModelLabel(t *testing.T) {
	blankBuiltinProviderCreds(t)
	t.Setenv("TEI_BASE_URL", "http://gpu-box:8080")
	cfg := loadCfg(t, "version: 1\nmodel:\n  embed:\n    provider: tei\n")
	id := cfg.Providers().EmbedIdentity()
	if !strings.HasPrefix(id, "tei|http://gpu-box:8080|tei|tei|") {
		t.Fatalf("identity %q must fall back to the tei model label", id)
	}
}

// TestProviders_TEITransportRule pins SPEC §8.1.1's transport rule for tei:
// late chunking sends whole documents to the endpoint, so a REMOTE endpoint
// must be https (plain http to a public host is CONFIG_INVALID), while loopback,
// private-network, link-local, .local/.internal and single-label LAN hosts stay
// allowed over http, exactly as §8.5 permits for self-hosted kinds.
func TestProviders_TEITransportRule(t *testing.T) {
	blankBuiltinProviderCreds(t)
	for _, tc := range []struct {
		url  string
		want bool // valid?
	}{
		{"http://localhost:8080", true},
		{"http://127.0.0.1:8080", true},
		{"http://10.1.2.3:8080", true},
		{"http://192.168.1.20:8080", true},
		{"http://gpu-box:8080", true},
		{"http://tei.internal:8080", true},
		{"https://embed.example.com", true},
		{"http://embed.example.com", false},
		{"http://203.0.113.9:8080", false},
	} {
		t.Setenv("TEI_BASE_URL", tc.url)
		// LoadFile validates, so the rule fires at load time.
		path := filepath.Join(t.TempDir(), ".dir2mcp.yaml")
		writeFile(t, path, "version: 1\nmodel:\n  embed:\n    provider: tei\n")
		_, err := config.LoadFile(path)
		if tc.want && err != nil {
			t.Errorf("%s: want valid, got %v", tc.url, err)
		}
		if !tc.want && (err == nil || !strings.Contains(err.Error(), "CONFIG_INVALID") || !strings.Contains(err.Error(), "https")) {
			t.Errorf("%s: want a CONFIG_INVALID naming https, got %v", tc.url, err)
		}
	}
}
