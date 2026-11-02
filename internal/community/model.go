// Package community owns the multi-community domain. Its transactions do not
// depend on Redis or Kafka availability; outbox delivery happens after commit.
package community

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string            { return e.Message }
func fail(code int, message string) error { return &Error{code, message} }
func Status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 503
}
func invalid(s string) error   { return fail(http.StatusBadRequest, s) }
func denied() error            { return fail(http.StatusNotFound, "resource unavailable or membership required") }
func conflict(s string) error  { return fail(http.StatusConflict, s) }
func manager(role string) bool { return role == "owner" || role == "admin" }
func bounded(s string, min, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= min && n <= max
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type Community struct {
	ID          string `json:"group_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       string `json:"owner_id"`
	Role        string `json:"role"`
	Members     int    `json:"members"`
}
type Input struct {
	RequestID       string   `json:"request_id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Kind            string   `json:"kind"`
	Title           string   `json:"title"`
	Body            string   `json:"body"`
	ExpectedVersion int64    `json:"expected_version"`
	Status          string   `json:"status"`
	Pinned          bool     `json:"pinned"`
	Featured        bool     `json:"featured"`
	ParentID        string   `json:"parent_id"`
	AttachmentIDs   []string `json:"attachment_ids"`
	UserID          string   `json:"user_id"`
}
type Post struct {
	ID          string       `json:"post_id"`
	GroupID     string       `json:"group_id"`
	AuthorID    string       `json:"author_id"`
	Kind        string       `json:"kind"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	Status      string       `json:"status"`
	Pinned      bool         `json:"pinned"`
	Featured    bool         `json:"featured"`
	Version     int64        `json:"version"`
	CreatedAt   int64        `json:"created_at"`
	UpdatedAt   int64        `json:"updated_at"`
	Likes       int          `json:"likes"`
	Liked       bool         `json:"liked"`
	Comments    int          `json:"comments"`
	Attachments []Attachment `json:"attachments"`
}
type Attachment struct {
	ID   string `json:"attachment_id"`
	Name string `json:"file_name"`
	Size int64  `json:"size"`
}
type Comment struct {
	ID        string `json:"comment_id"`
	Author    string `json:"author_id"`
	Parent    string `json:"parent_id"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
}
type Event struct {
	ID        string `json:"event_id"`
	GroupID   string `json:"group_id"`
	Actor     string `json:"actor_id"`
	Kind      string `json:"kind"`
	ObjectID  string `json:"object_id"`
	Recipient string `json:"recipient_id"`
	CreatedAt int64  `json:"created_at"`
}
