package tests

import (
	"testing"

	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/provider"
	"github.com/dirstral/dir2mcp/internal/providerfactory"
	"github.com/dirstral/dir2mcp/internal/tei"
)

// TestEmbedder_TEI pins dir2mcp#565: the self-hosted tei kind builds a
// model.Embedder that also satisfies model.TokenEmbedder, so the embedding
// worker's late-chunking gate (latechunk.Decide) activates for it (SPEC 8.1.9).
// It is credential-optional (a bare base_url is enough) and the profile's
// embed_text_model becomes the client's identity label.
func TestEmbedder_TEI(t *testing.T) {
	p := provider.Profile{
		Name: "tei", Kind: provider.KindTEI,
		BaseURL: "http://gpu-box:8080", CredentialLess: true,
		EmbedTextModel: "sentence-transformers/all-MiniLM-L6-v2",
	}
	e, err := providerfactory.Embedder(p)
	if err != nil {
		t.Fatalf("Embedder(tei) error: %v", err)
	}
	c, ok := e.(*tei.Client)
	if !ok {
		t.Fatalf("Embedder(tei) = %T, want *tei.Client", e)
	}
	if _, ok := e.(model.TokenEmbedder); !ok {
		t.Fatal("tei embedder must satisfy model.TokenEmbedder (SPEC 8.1.9)")
	}
	if c.DefaultEmbedModel != "sentence-transformers/all-MiniLM-L6-v2" {
		t.Fatalf("DefaultEmbedModel = %q, want the profile's embed_text_model", c.DefaultEmbedModel)
	}
	if c.BaseURL != "http://gpu-box:8080" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

// TestEmbedder_TEIRejectsFixedDimension pins that embed.text_dim/code_dim stay
// CONFIG_INVALID for tei (a TEI server serves its model's native dimension).
func TestEmbedder_TEIRejectsFixedDimension(t *testing.T) {
	p := provider.Profile{Name: "tei", Kind: provider.KindTEI, BaseURL: "http://gpu-box:8080", CredentialLess: true, EmbedTextDim: 256}
	if _, err := providerfactory.Embedder(p); err == nil {
		t.Fatal("a fixed output dimension must be rejected for kind tei")
	}
}

// TestKindDefaultEmbedModel_TEI pins the drift guard between the provider
// package's identity table and the client's constant (issue #705 pattern).
func TestKindDefaultEmbedModel_TEI(t *testing.T) {
	if provider.KindDefaultEmbedModel(provider.KindTEI) != tei.DefaultModel {
		t.Fatalf("provider.DefaultTEIModel %q != tei.DefaultModel %q", provider.KindDefaultEmbedModel(provider.KindTEI), tei.DefaultModel)
	}
	if !provider.IsKnownKind(provider.KindTEI) {
		t.Fatal("tei must be a known kind (SPEC 8.1.1)")
	}
	if provider.Can(provider.KindTEI, provider.CapEmbed) != provider.Supported {
		t.Fatal("tei must be statically embed-capable (SPEC 8.1.2)")
	}
	for _, cap := range []provider.Capability{provider.CapChat, provider.CapOCR, provider.CapSTT, provider.CapTTS, provider.CapRerank} {
		if provider.Can(provider.KindTEI, cap) != provider.Unsupported {
			t.Fatalf("tei must not serve %s", cap)
		}
	}
}
