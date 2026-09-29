package kql

import "testing"

func TestLexKQLForms(t *testing.T) {
	source := "\ufeff// comment\nT | parse-where s with @'a''b' | where x !in~ (1, 2h)"
	tokens, errs := Lex(source)
	if len(errs) != 0 {
		t.Fatalf("Lex() errors = %v", errs)
	}
	want := []string{"T", "|", "parse-where", "s", "with", "@'a''b'", "|", "where", "x", "!in~", "(", "1", ",", "2h", ")", ""}
	if len(tokens) != len(want) {
		t.Fatalf("token count = %d, want %d: %#v", len(tokens), len(want), tokens)
	}
	for i := range want {
		if tokens[i].Text != want[i] {
			t.Errorf("token %d = %q, want %q", i, tokens[i].Text, want[i])
		}
	}
}

func TestLexMultilineAndGraphTokens(t *testing.T) {
	tokens, errs := Lex("print s=```a\nb```; Edges | make-graph source --> target")
	if len(errs) != 0 {
		t.Fatalf("Lex() errors = %v", errs)
	}
	found := map[string]bool{}
	for _, token := range tokens {
		found[token.Text] = true
	}
	for _, text := range []string{"```a\nb```", "make-graph", "-->"} {
		if !found[text] {
			t.Errorf("missing token %q", text)
		}
	}
}

func TestLexReportsUnterminatedString(t *testing.T) {
	_, errs := Lex("print 'broken")
	if len(errs) != 1 || errs[0].Message != "unterminated string" {
		t.Fatalf("Lex() errors = %#v", errs)
	}
}
