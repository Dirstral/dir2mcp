package tests

import (
	"strings"
	"testing"
)

// The script half of #957. Measured on an eight-recording Ukrainian / Kyrgyz /
// Russian / Georgian archive, three English questions asked six times each:
// the shipped rule and reminder produced 13 of 18 answers in English, and the
// answers that drifted were in the language of the retrieved CONTEXT. Naming the
// question's SCRIPT in the reminder took it to 18 of 18. Naming its LANGUAGE was
// rejected: the shipped trigram detector calls "What is said about Crimea in
// these recordings?" Afrikaans.
//
// These tests pin the prompt-side contract. A cross-lingual end-to-end
// measurement needs a live chat provider, so it lives in the corpus harness.

const scriptMarker = "alphabet, and the answer must be written in that alphabet too"

func TestAsk957_ReminderNamesTheScriptOfALatinQuestion(t *testing.T) {
	prompt := askAndCapture(t, "")
	if !strings.Contains(prompt, "written in the Latin alphabet") {
		t.Fatalf("the reminder must name the question's script:\n%s", prompt)
	}
	if !strings.Contains(prompt, scriptMarker) {
		t.Fatalf("the reminder must bind the answer to that script:\n%s", prompt)
	}
}

// The sentence must sit inside the trailing reminder, after the context. #892
// established that position as the fix; a script rule stated before a long block
// of foreign text would be in exactly the place that already failed.
func TestAsk957_ScriptSentenceIsInTheTrailingReminder(t *testing.T) {
	prompt := askAndCapture(t, "")
	idx := strings.Index(prompt, reminderMarker)
	if idx < 0 {
		t.Fatal("no trailing reminder")
	}
	if !strings.Contains(prompt[idx:], scriptMarker) {
		t.Fatalf("the script sentence must be part of the reminder:\n%s", prompt[idx:])
	}
}

// A quoted passage keeps its own spelling. Without this the instruction would
// push the model to transliterate the very evidence it is citing.
func TestAsk957_ScriptSentenceAllowsQuotedText(t *testing.T) {
	prompt := askAndCapture(t, "")
	if !strings.Contains(prompt, "Quoted words from a document keep their own spelling") {
		t.Fatalf("the script rule must exempt quoted text:\n%s", prompt)
	}
}

// An operator who replaced the prompt is not given the reminder at all (#892),
// so they are not given the script sentence either: it is part of the same
// instruction and must not contradict an operator who fixed one answer language.
func TestAsk957_ReplacedPromptGetsNoScriptSentence(t *testing.T) {
	prompt := askAndCapture(t, "Answer in German at all times. Cite the file.")
	if strings.Contains(prompt, scriptMarker) {
		t.Fatalf("a replaced prompt must not receive the script sentence:\n%s", prompt)
	}
}
