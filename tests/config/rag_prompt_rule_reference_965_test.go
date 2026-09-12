package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dirstral/dir2mcp/internal/config"
	"github.com/dirstral/dir2mcp/internal/promptrules"
)

// Issue #965: an operator who COPIES a shipped prompt rule into
// rag.system_prompt loses the behaviour keyed on it the next time the rule is
// reworded, and nothing says so.
//
// Measured on four deployments right after #963 added one clause to the
// answer-language rule: three reproduced the previous wording exactly, so that
// they would keep the trailing reminder of #892, and all three stopped matching
// in that commit. Config load passed, the daemon started, ask answered, and only
// the drift rate changed.
//
// Two answers, at two strengths. A reference (`${rag.answer_language_rule}`)
// cannot go stale, because it names the rule instead of restating it. A copy
// that has ALREADY gone stale is reported at load, because the operators who
// need that sentence wrote their configs before the reference existed.

// staleAnswerLanguageCopy is the answer-language rule as it read before #963:
// the current rule minus the proper-noun clause that commit added. It is
// DERIVED from the shipped text rather than pasted, so this fixture cannot
// become the very thing it tests.
func staleAnswerLanguageCopy() string {
	return strings.Replace(promptrules.AnswerLanguageRule, promptrules.ProperNounClause, "", 1)
}

func warningsOf(t *testing.T, prompt string) []string {
	t.Helper()
	cfg := config.Default()
	cfg.RAGSystemPrompt = prompt
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out := make([]string, 0, len(cfg.Warnings))
	for _, w := range cfg.Warnings {
		out = append(out, w.Error())
	}
	return out
}

func warningMentioning(warnings []string, needle string) string {
	for _, w := range warnings {
		if strings.Contains(w, needle) {
			return w
		}
	}
	return ""
}

// TestConfig965_StaleCopyOfTheAnswerLanguageRuleIsReported is the detection
// half. The operator is told three things: that the copy is partial, what
// stopped working, and what to write instead.
func TestConfig965_StaleCopyOfTheAnswerLanguageRuleIsReported(t *testing.T) {
	prompt := "You answer questions about baseball broadcasts.\n" +
		staleAnswerLanguageCopy() + promptrules.CitationRule

	warn := warningMentioning(warningsOf(t, prompt), promptrules.AnswerLanguageRuleName)
	if warn == "" {
		t.Fatalf("a pre-#963 copy of the answer-language rule produced no warning; warnings: %v",
			warningsOf(t, prompt))
	}
	for _, want := range []string{
		promptrules.AnswerLanguageRuleToken, // what to write instead
		"#892",                              // what stopped working
		strings.TrimSpace(promptrules.ProperNounClause), // the current text, named
	} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning does not mention %q:\n%s", want, warn)
		}
	}
}

// TestConfig965_ACurrentCopyIsNotReported keeps the warning honest. A prompt
// that carries today's rule word for word works today, and a warning on it
// would be noise an operator learns to skip.
func TestConfig965_ACurrentCopyIsNotReported(t *testing.T) {
	prompt := "You answer questions about baseball broadcasts.\n" +
		promptrules.AnswerLanguageRule + promptrules.CitationRule
	if warnings := warningsOf(t, prompt); len(warnings) != 0 {
		t.Fatalf("a current copy must not warn: %v", warnings)
	}
}

// TestConfig965_AWrappedStaleCopyIsStillReported: a prompt pasted into a YAML
// block scalar comes back rewrapped. The words are the same and the copy is
// just as stale, so the report must survive the line breaks. It uses the same
// whitespace-insensitive comparison the #892 gate itself uses.
func TestConfig965_AWrappedStaleCopyIsStillReported(t *testing.T) {
	wrapped := strings.ReplaceAll(staleAnswerLanguageCopy(), " ", "\n  ")
	if warningMentioning(warningsOf(t, wrapped), promptrules.AnswerLanguageRuleName) == "" {
		t.Fatal("a rewrapped stale copy was not recognized")
	}
}

// TestConfig965_AnOperatorsOwnRuleIsNotReported is the false-positive guard.
// An operator who fixed one answer language wrote their own sentence and copied
// nothing, so there is nothing to report and nothing that will go stale.
func TestConfig965_AnOperatorsOwnRuleIsNotReported(t *testing.T) {
	for _, prompt := range []string{
		"Answer from the provided context only. Always answer in German, whatever " +
			"language the question uses. Cite sources as [rel_path].",
		"You answer questions about city planning. Reply in the language of the question.",
		"Answer briefly and cite the file.",
	} {
		if warnings := warningsOf(t, prompt); len(warnings) != 0 {
			t.Errorf("prompt %q copies nothing, so it must not warn: %v", prompt, warnings)
		}
	}
}

// TestConfig965_AReferenceIsNeverReportedAsStale: the reference is the fix, so
// a prompt that uses it must be silent. It carries no copy, by construction.
func TestConfig965_AReferenceIsNeverReportedAsStale(t *testing.T) {
	prompt := "You answer questions about baseball broadcasts.\n" +
		promptrules.AnswerLanguageRuleToken + "\n" + promptrules.CitationRuleToken
	if warnings := warningsOf(t, prompt); len(warnings) != 0 {
		t.Fatalf("a referenced rule must not warn: %v", warnings)
	}
}

// TestConfig965_AnUnknownReferenceIsConfigInvalid closes the typo hole. Left as
// literal text, a near-miss like `${rag.answer_language}` would travel to the
// model in place of the rule, and the reminder would stand down exactly as it
// does for a stale copy. The namespace is new, so nothing existing can break.
func TestConfig965_AnUnknownReferenceIsConfigInvalid(t *testing.T) {
	cfg := config.Default()
	cfg.RAGSystemPrompt = "Answer briefly.\n${rag.answer_language}\n"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("a reference that names no shipped rule must be CONFIG_INVALID")
	}
	for _, want := range []string{"CONFIG_INVALID", "${rag.answer_language}", promptrules.AnswerLanguageRuleToken} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

// TestConfig965_TextOutsideTheNamespaceIsPromptText: only `${rag.*}` is owned.
// A prompt that talks about shell or template syntax is prompt text and must
// load.
func TestConfig965_TextOutsideTheNamespaceIsPromptText(t *testing.T) {
	cfg := config.Default()
	cfg.RAGSystemPrompt = "Explain what ${HOME} and ${ragged_edge} mean in the documents."
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a prompt mentioning an unrelated ${...} must load: %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", cfg.Warnings)
	}
}

// TestConfig965_TheWarningIsEmittedOnce: Validate runs more than once on one
// Config (`up` re-validates after merging its flags, the save paths validate
// before writing). The operator reads the warning once.
func TestConfig965_TheWarningIsEmittedOnce(t *testing.T) {
	cfg := config.Default()
	cfg.RAGSystemPrompt = staleAnswerLanguageCopy()
	for i := 0; i < 3; i++ {
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate #%d: %v", i, err)
		}
	}
	if len(cfg.Warnings) != 1 {
		t.Fatalf("want exactly one warning after three Validate calls, got %d: %v",
			len(cfg.Warnings), cfg.Warnings)
	}
}

// TestConfig965_AReferenceIsPersistedUnexpanded is the property the whole fix
// rests on. If saving a config wrote the RULE where the operator wrote the
// reference, the file would hold a copy again, pinned to the version that wrote
// it, and the next rewording would break it in silence.
func TestConfig965_AReferenceIsPersistedUnexpanded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".dir2mcp.yaml")

	cfg := config.Default()
	cfg.RAGSystemPrompt = "Answer about contracts. " + promptrules.AnswerLanguageRuleToken
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(raw), promptrules.AnswerLanguageRuleToken) {
		t.Fatalf("the saved config lost the reference:\n%s", raw)
	}
	if strings.Contains(string(raw), "This instruction fixes the answer language") {
		t.Fatalf("the saved config expanded the reference into a copy:\n%s", raw)
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !strings.Contains(loaded.RAGSystemPrompt, promptrules.AnswerLanguageRuleToken) {
		t.Fatalf("the loaded config lost the reference: %q", loaded.RAGSystemPrompt)
	}
}
