package database

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrQuestionNotFound = errors.New("question not found")

type ChallengeDefinition struct {
	Flag      string
	Cooldown  time.Duration
	Questions []QuestionDefinition
}

type QuestionDefinition struct {
	ID              string
	Revision        int
	Title           string
	Prompt          string
	AcceptedAnswers []string
	CaseSensitive   bool
}

type QuestionState struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Prompt   string     `json:"prompt"`
	Attempts int        `json:"attempts"`
	Solved   bool       `json:"solved"`
	SolvedAt *time.Time `json:"solvedAt"`
	Answer   string     `json:"answer,omitempty"`
}

type ChallengeState struct {
	Questions []QuestionState `json:"questions"`
	Solved    int             `json:"solvedQuestions"`
	Total     int             `json:"totalQuestions"`
	Completed bool            `json:"completed"`
	Flag      string          `json:"flag,omitempty"`
}

type AnswerResult struct {
	Correct       bool           `json:"correct"`
	AlreadySolved bool           `json:"alreadySolved"`
	RetryAfter    time.Duration  `json:"-"`
	State         ChallengeState `json:"state"`
}

func (s *Store) ConfigureChallenge(ctx context.Context, challenge ChallengeDefinition) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin challenge configuration: %w", err)
	}
	defer tx.Rollback()

	retained := make(map[string]struct{}, len(challenge.Questions))
	for _, question := range challenge.Questions {
		retained[question.ID] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO investigation_progress(question_id, revision) VALUES (?, ?)
ON CONFLICT(question_id) DO UPDATE SET
    revision = excluded.revision,
    attempts = CASE WHEN revision <> excluded.revision THEN 0 ELSE attempts END,
    last_attempt_at = CASE WHEN revision <> excluded.revision THEN NULL ELSE last_attempt_at END,
    solved_at = CASE WHEN revision <> excluded.revision THEN NULL ELSE solved_at END,
    answer = CASE WHEN revision <> excluded.revision THEN NULL ELSE answer END`, question.ID, question.Revision); err != nil {
			return fmt.Errorf("configure question %q: %w", question.ID, err)
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT question_id FROM investigation_progress")
	if err != nil {
		return fmt.Errorf("list configured questions: %w", err)
	}
	var removed []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan configured question: %w", err)
		}
		if _, keep := retained[id]; !keep {
			removed = append(removed, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list configured questions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close configured questions: %w", err)
	}
	for _, id := range removed {
		if _, err := tx.ExecContext(ctx, "DELETE FROM investigation_progress WHERE question_id = ?", id); err != nil {
			return fmt.Errorf("remove question %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit challenge configuration: %w", err)
	}

	s.challengeMu.Lock()
	s.challenge = challenge
	s.challengeMu.Unlock()
	return nil
}

func (s *Store) ChallengeState(ctx context.Context) (ChallengeState, error) {
	s.challengeMu.RLock()
	challenge := s.challenge
	s.challengeMu.RUnlock()

	state := ChallengeState{Questions: make([]QuestionState, 0, len(challenge.Questions)), Total: len(challenge.Questions)}
	for _, question := range challenge.Questions {
		current := QuestionState{ID: question.ID, Title: question.Title, Prompt: question.Prompt}
		var solvedAt, answer sql.NullString
		err := s.db.QueryRowContext(ctx, `
SELECT attempts, solved_at, answer
FROM investigation_progress
WHERE question_id = ? AND revision = ?`, question.ID, question.Revision).Scan(&current.Attempts, &solvedAt, &answer)
		if errors.Is(err, sql.ErrNoRows) {
			return ChallengeState{}, fmt.Errorf("question %q progress is unavailable", question.ID)
		}
		if err != nil {
			return ChallengeState{}, fmt.Errorf("get question %q progress: %w", question.ID, err)
		}
		if solvedAt.Valid {
			parsed, err := time.Parse(time.RFC3339Nano, solvedAt.String)
			if err != nil {
				return ChallengeState{}, fmt.Errorf("parse question %q solved time: %w", question.ID, err)
			}
			current.Solved = true
			current.SolvedAt = &parsed
			current.Answer = answer.String
			state.Solved++
		}
		state.Questions = append(state.Questions, current)
	}
	state.Completed = state.Total > 0 && state.Solved == state.Total
	if state.Completed {
		state.Flag = challenge.Flag
	}
	return state, nil
}

func (s *Store) SubmitAnswer(ctx context.Context, questionID, answer string, now time.Time) (AnswerResult, error) {
	s.challengeMu.RLock()
	challenge := s.challenge
	s.challengeMu.RUnlock()
	var question *QuestionDefinition
	for index := range challenge.Questions {
		if challenge.Questions[index].ID == questionID {
			question = &challenge.Questions[index]
			break
		}
	}
	if question == nil {
		return AnswerResult{}, ErrQuestionNotFound
	}

	answer = strings.TrimSpace(answer)
	normalized := normalizeAnswer(answer, question.CaseSensitive)
	normalizedHash := sha256.Sum256([]byte(normalized))
	correct := false
	for _, accepted := range question.AcceptedAnswers {
		candidate := normalizeAnswer(accepted, question.CaseSensitive)
		candidateHash := sha256.Sum256([]byte(candidate))
		if subtle.ConstantTimeCompare(candidateHash[:], normalizedHash[:]) == 1 {
			correct = true
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AnswerResult{}, fmt.Errorf("begin answer submission: %w", err)
	}
	defer tx.Rollback()
	var attempts int
	var lastAttempt, solvedAt, storedAnswer sql.NullString
	if err := tx.QueryRowContext(ctx, `
SELECT attempts, last_attempt_at, solved_at, answer
FROM investigation_progress
WHERE question_id = ? AND revision = ?`, question.ID, question.Revision).Scan(&attempts, &lastAttempt, &solvedAt, &storedAnswer); err != nil {
		return AnswerResult{}, fmt.Errorf("get question progress: %w", err)
	}
	if solvedAt.Valid {
		if !storedAnswer.Valid {
			if !correct {
				if err := tx.Commit(); err != nil {
					return AnswerResult{}, fmt.Errorf("commit answer submission: %w", err)
				}
				state, err := s.ChallengeState(ctx)
				return AnswerResult{AlreadySolved: true, State: state}, err
			}
			if _, err := tx.ExecContext(ctx, `
UPDATE investigation_progress SET answer = ?
WHERE question_id = ? AND revision = ?`, answer, question.ID, question.Revision); err != nil {
				return AnswerResult{}, fmt.Errorf("store solved question answer: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return AnswerResult{}, fmt.Errorf("commit answer submission: %w", err)
		}
		state, err := s.ChallengeState(ctx)
		return AnswerResult{Correct: true, AlreadySolved: true, State: state}, err
	}
	if lastAttempt.Valid && challenge.Cooldown > 0 {
		last, err := time.Parse(time.RFC3339Nano, lastAttempt.String)
		if err != nil {
			return AnswerResult{}, fmt.Errorf("parse last attempt time: %w", err)
		}
		if remaining := challenge.Cooldown - now.Sub(last); remaining > 0 {
			return AnswerResult{RetryAfter: remaining}, nil
		}
	}
	formattedNow := now.UTC().Format(time.RFC3339Nano)
	var solved any
	var acceptedAnswer any
	if correct {
		solved = formattedNow
		acceptedAnswer = answer
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE investigation_progress
SET attempts = attempts + 1,
    last_attempt_at = ?,
    solved_at = COALESCE(?, solved_at),
    answer = COALESCE(?, answer)
WHERE question_id = ? AND revision = ?`, formattedNow, solved, acceptedAnswer, question.ID, question.Revision); err != nil {
		return AnswerResult{}, fmt.Errorf("update question progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AnswerResult{}, fmt.Errorf("commit answer submission: %w", err)
	}
	state, err := s.ChallengeState(ctx)
	return AnswerResult{Correct: correct, State: state}, err
}

func normalizeAnswer(value string, caseSensitive bool) string {
	value = strings.TrimSpace(value)
	if !caseSensitive {
		value = strings.ToLower(value)
	}
	return value
}
