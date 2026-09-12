package tests

import (
	"strings"
	"testing"

	"github.com/dirstral/dir2mcp/internal/retrieval"
)

// retrieval.ScriptReminderForQuestion is the unit behind the sentence appended
// to the #892 reminder. The rules it has to get right are: name a script only
// when the question really is in one, stay silent when it is not, and never let
// a stray foreign name decide.
func TestScript957_NamesTheScriptOfAClearQuestion(t *testing.T) {
	cases := map[string]string{
		"What is said about Crimea in these recordings?":    "Latin",
		"Что говорится в этих записях о Крыме?":             "Cyrillic",
		"Хто такий патріарх Філарет і про що він говорить?": "Cyrillic",
		"Садыр Жапаров референдум жөнүндө эмне дейт?":       "Cyrillic",
		"რა ითქვა ამ ჩანაწერებში ყირიმის შესახებ?":          "Georgian",
		"這些錄音中關於克里米亞說了什麼？":                                  "Han",
		"ماذا قيل عن القرم في هذه التسجيلات؟":               "Arabic",
	}
	for q, want := range cases {
		got := retrieval.ScriptReminderForQuestion(q)
		if !strings.Contains(got, "written in the "+want+" alphabet") {
			t.Errorf("question %q: want the %s alphabet named, got %q", q, want, got)
		}
	}
}

// A foreign proper noun inside an otherwise Latin question must not flip the
// verdict. This is the same trigger #957 reported for the language rule, seen
// from the script side.
func TestScript957_AForeignNameDoesNotFlipTheScript(t *testing.T) {
	got := retrieval.ScriptReminderForQuestion("When is Рафаэль Деверс on screen in the broadcast?")
	if !strings.Contains(got, "written in the Latin alphabet") {
		t.Errorf("a Latin question with one Cyrillic name is still Latin, got %q", got)
	}
}

// Genuinely mixed input has no dominant script, and saying nothing is better
// than naming the wrong one.
func TestScript957_SaysNothingWhenNoScriptDominates(t *testing.T) {
	if got := retrieval.ScriptReminderForQuestion("Крым Crimea Крым Crimea"); got != "" {
		t.Errorf("a half-and-half question must get no script sentence, got %q", got)
	}
}

// Too little letter content to judge: a question of three letters, or of digits
// and punctuation, gets no instruction rather than a guess.
func TestScript957_SaysNothingOnTooLittleText(t *testing.T) {
	for _, q := range []string{"NATO?", "2020?", "", "   ", "1 + 1 = ?"} {
		if got := retrieval.ScriptReminderForQuestion(q); got != "" {
			t.Errorf("question %q must get no script sentence, got %q", q, got)
		}
	}
}
