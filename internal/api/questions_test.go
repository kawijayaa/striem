package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/database"
)

func TestInvestigationQuestionWorkflow(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ConfigureChallenge(t.Context(), database.ChallengeDefinition{
		Flag: "flag{complete}",
		Questions: []database.QuestionDefinition{
			{ID: "source", Revision: 1, Title: "Source", Prompt: "Which source?", AcceptedAnswers: []string{"192.0.2.1"}},
			{ID: "user", Revision: 1, Title: "User", Prompt: "Which user?", AcceptedAnswers: []string{"Admin"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/questions")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || bytes.Contains(initial, []byte("192.0.2.1")) || bytes.Contains(initial, []byte("flag{complete}")) {
		t.Fatalf("initial questions status=%d body=%s", response.StatusCode, initial)
	}

	incorrect := submitQuestion(t, server.URL, "source", "wrong")
	if incorrect.StatusCode != http.StatusOK || incorrect.Body.Correct {
		t.Fatalf("incorrect submission = %#v", incorrect)
	}
	first := submitQuestion(t, server.URL, "source", " 192.0.2.1 ")
	if first.StatusCode != http.StatusOK || !first.Body.Correct || first.Body.State.Completed || first.Body.State.Flag != "" || first.Body.State.Questions[0].Answer != "192.0.2.1" {
		t.Fatalf("first solve = %#v", first)
	}
	final := submitQuestion(t, server.URL, "user", "ADMIN")
	if final.StatusCode != http.StatusOK || !final.Body.Correct || !final.Body.State.Completed || final.Body.State.Flag != "flag{complete}" {
		t.Fatalf("final solve = %#v", final)
	}
	repeated := submitQuestion(t, server.URL, "user", "anything")
	if !repeated.Body.Correct || !repeated.Body.AlreadySolved {
		t.Fatalf("repeated submission = %#v", repeated)
	}
}

func TestInvestigationQuestionCooldown(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ConfigureChallenge(t.Context(), database.ChallengeDefinition{
		Flag: "flag", Cooldown: time.Minute,
		Questions: []database.QuestionDefinition{{ID: "answer", Revision: 1, Title: "Answer", Prompt: "Answer?", AcceptedAnswers: []string{"yes"}}},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	if response := submitQuestion(t, server.URL, "answer", "no"); response.StatusCode != http.StatusOK {
		t.Fatalf("first submission status = %d", response.StatusCode)
	}
	response := submitQuestion(t, server.URL, "answer", "yes")
	if response.StatusCode != http.StatusTooManyRequests || response.RetryAfterMs <= 0 {
		t.Fatalf("cooldown response = %#v", response)
	}
}

type questionSubmissionResponse struct {
	StatusCode   int
	RetryAfterMs int64
	Body         struct {
		Correct       bool                    `json:"correct"`
		AlreadySolved bool                    `json:"alreadySolved"`
		State         database.ChallengeState `json:"state"`
	}
}

func submitQuestion(t *testing.T, baseURL, id, answer string) questionSubmissionResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/questions/"+id+"/answer", strings.NewReader(string(mustJSON(t, map[string]string{"answer": answer}))))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Striem-Request", "1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result questionSubmissionResponse
	result.StatusCode = response.StatusCode
	if response.StatusCode == http.StatusTooManyRequests {
		var body struct {
			RetryAfterMs int64 `json:"retryAfterMs"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		result.RetryAfterMs = body.RetryAfterMs
		return result
	}
	if err := json.NewDecoder(response.Body).Decode(&result.Body); err != nil {
		t.Fatal(err)
	}
	return result
}
