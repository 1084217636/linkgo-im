package learningtask

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

type Error struct {
	Code           int
	Message        string
	CurrentVersion *int64
}

func (e *Error) Error() string { return e.Message }

func bad(message string) error      { return &Error{Code: 400, Message: message} }
func missing() error                { return &Error{Code: 404, Message: "task or group not found"} }
func conflict(message string) error { return &Error{Code: 409, Message: message} }
func versionConflict(message string, current int64) error {
	return &Error{Code: 409, Message: message, CurrentVersion: &current}
}
func unavailable(err error) error { return fmt.Errorf("task store unavailable: %w", err) }

type Task struct {
	ID          string `json:"task_id"`
	GroupID     string `json:"group_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueAt       int64  `json:"due_at"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
	CreatedBy   string `json:"created_by"`
	PublishedAt int64  `json:"published_at"`
	ClosedAt    int64  `json:"closed_at"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type Submission struct {
	ID            string   `json:"submission_id"`
	TaskID        string   `json:"task_id"`
	UserID        string   `json:"user_id"`
	Text          string   `json:"text"`
	Link          string   `json:"link"`
	AttachmentIDs []string `json:"attachment_ids"`
	Status        string   `json:"status"`
	AttemptNo     int      `json:"attempt_no"`
	Version       int64    `json:"version"`
	ReviewerID    string   `json:"reviewer_id"`
	ReviewNote    string   `json:"review_note"`
	SubmittedAt   int64    `json:"submitted_at"`
	ReviewedAt    int64    `json:"reviewed_at"`
}

type Detail struct {
	Task         Task        `json:"task"`
	Role         string      `json:"role"`
	MySubmission *Submission `json:"my_submission,omitempty"`
}

type Progress struct {
	TaskID       string `json:"task_id"`
	Total        int64  `json:"total"`
	NotSubmitted int64  `json:"not_submitted"`
	Submitted    int64  `json:"submitted"`
	Returned     int64  `json:"returned"`
	Approved     int64  `json:"approved"`
}

type TaskList struct {
	Tasks      []Task `json:"tasks"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type SubmissionList struct {
	Submissions []Submission `json:"submissions"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

func newID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func requestHash(action, target string, payload any) string {
	b, _ := json.Marshal(struct {
		Action  string `json:"action"`
		Target  string `json:"target"`
		Payload any    `json:"payload"`
	}{action, target, payload})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validRequestID(v string) error {
	if len(v) < 8 || len(v) > 128 || strings.TrimSpace(v) != v {
		return bad("request_id must be 8-128 characters")
	}
	return nil
}

func validateTaskFields(title, description string, dueAt, now int64) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(title)); n < 1 || n > 120 {
		return bad("title must be 1-120 characters")
	}
	if utf8.RuneCountInString(description) > 5000 {
		return bad("description is too long")
	}
	if dueAt != 0 && dueAt <= now {
		return bad("due_at must be in the future")
	}
	return nil
}

func validateSubmission(text, link string, attachmentIDs []string) error {
	if strings.TrimSpace(text) == "" && strings.TrimSpace(link) == "" && len(attachmentIDs) == 0 {
		return bad("text, link or attachment is required")
	}
	if len(attachmentIDs) > 10 {
		return bad("at most 10 attachments are allowed")
	}
	seen := make(map[string]struct{}, len(attachmentIDs))
	for _, id := range attachmentIDs {
		if len(id) != 32 {
			return bad("invalid attachment id")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return bad("invalid attachment id")
		}
		if _, exists := seen[id]; exists {
			return bad("duplicate attachment id")
		}
		seen[id] = struct{}{}
	}
	if utf8.RuneCountInString(text) > 5000 {
		return bad("text is too long")
	}
	if len(link) > 2048 {
		return bad("link is too long")
	}
	if link != "" {
		u, err := url.Parse(link)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return bad("link must be an http or https URL")
		}
	}
	return nil
}

func statusCode(err error) int {
	var taskErr *Error
	if errors.As(err, &taskErr) {
		return taskErr.Code
	}
	return 503
}

// StatusCode maps domain errors to the HTTP API without exposing SQL details.
func StatusCode(err error) int { return statusCode(err) }
