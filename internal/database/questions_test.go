package database

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestChallengeProgressAndCompletion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	challenge := ChallengeDefinition{
		Flag: "flag{complete}", Cooldown: 2 * time.Second,
		Questions: []QuestionDefinition{
			{ID: "source-ip", Revision: 1, Title: "Source", Prompt: "Which IP?", AcceptedAnswers: []string{"192.0.2.1"}},
			{ID: "account", Revision: 1, Title: "Account", Prompt: "Which account?", AcceptedAnswers: []string{"Admin", "administrator"}},
		},
	}
	if err := store.ConfigureChallenge(t.Context(), challenge); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)
	incorrect, err := store.SubmitAnswer(t.Context(), "source-ip", "wrong", now)
	if err != nil || incorrect.Correct || incorrect.State.Questions[0].Attempts != 1 {
		t.Fatalf("incorrect submission = %#v, %v", incorrect, err)
	}
	cooldown, err := store.SubmitAnswer(t.Context(), "source-ip", "192.0.2.1", now.Add(time.Second))
	if err != nil || cooldown.RetryAfter != time.Second {
		t.Fatalf("cooldown submission = %#v, %v", cooldown, err)
	}
	first, err := store.SubmitAnswer(t.Context(), "source-ip", " 192.0.2.1 ", now.Add(2*time.Second))
	if err != nil || !first.Correct || first.State.Completed || first.State.Flag != "" || first.State.Questions[0].Answer != "192.0.2.1" {
		t.Fatalf("first solve = %#v, %v", first, err)
	}
	final, err := store.SubmitAnswer(t.Context(), "account", "ADMIN", now.Add(4*time.Second))
	if err != nil || !final.Correct || !final.State.Completed || final.State.Flag != "flag{complete}" {
		t.Fatalf("final solve = %#v, %v", final, err)
	}
	repeated, err := store.SubmitAnswer(t.Context(), "account", "anything", now.Add(5*time.Second))
	if err != nil || !repeated.Correct || !repeated.AlreadySolved || repeated.State.Flag != "flag{complete}" {
		t.Fatalf("repeated solve = %#v, %v", repeated, err)
	}
	if _, err := store.SubmitAnswer(t.Context(), "missing", "answer", now); !errors.Is(err, ErrQuestionNotFound) {
		t.Fatalf("unknown question error = %v", err)
	}
}

func TestChallengeRevisionResetsProgress(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	question := QuestionDefinition{ID: "answer", Revision: 1, Title: "Answer", Prompt: "Answer?", AcceptedAnswers: []string{"yes"}}
	if err := store.ConfigureChallenge(t.Context(), ChallengeDefinition{Flag: "flag", Questions: []QuestionDefinition{question}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitAnswer(t.Context(), "answer", "yes", time.Now()); err != nil {
		t.Fatal(err)
	}
	question.Revision = 2
	if err := store.ConfigureChallenge(t.Context(), ChallengeDefinition{Flag: "flag", Questions: []QuestionDefinition{question}}); err != nil {
		t.Fatal(err)
	}
	state, err := store.ChallengeState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Solved != 0 || state.Questions[0].Attempts != 0 || state.Questions[0].Answer != "" {
		t.Fatalf("revision did not reset progress: %#v", state)
	}
}

func TestSolvedQuestionCanRecoverLegacyAnswer(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	question := QuestionDefinition{ID: "answer", Revision: 1, Title: "Answer", Prompt: "Answer?", AcceptedAnswers: []string{"yes"}}
	if err := store.ConfigureChallenge(t.Context(), ChallengeDefinition{Questions: []QuestionDefinition{question}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SubmitAnswer(t.Context(), "answer", "yes", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec("UPDATE investigation_progress SET answer = NULL WHERE question_id = 'answer'"); err != nil {
		t.Fatal(err)
	}
	incorrect, err := store.SubmitAnswer(t.Context(), "answer", "no", time.Now())
	if err != nil || incorrect.Correct || incorrect.State.Questions[0].Answer != "" {
		t.Fatalf("legacy incorrect answer = %#v, %v", incorrect, err)
	}
	correct, err := store.SubmitAnswer(t.Context(), "answer", " yes ", time.Now())
	if err != nil || !correct.Correct || correct.State.Questions[0].Answer != "yes" {
		t.Fatalf("legacy recovered answer = %#v, %v", correct, err)
	}
}
