package learningtask

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Service struct{ DB *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{DB: db} }

type rowScanner interface{ Scan(...any) error }
type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type notifyTarget struct{ kind, id string }

func scanTask(r rowScanner) (Task, error) {
	var t Task
	err := r.Scan(&t.ID, &t.GroupID, &t.Title, &t.Description, &t.DueAt, &t.Status, &t.Version, &t.CreatedBy, &t.PublishedAt, &t.ClosedAt, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

const taskColumns = `task_id,group_id,title,description,COALESCE(due_at,0),status,version,created_by,COALESCE(published_at,0),COALESCE(closed_at,0),created_at,updated_at`

func scanSubmission(r rowScanner) (Submission, error) {
	var s Submission
	var attachmentJSON []byte
	err := r.Scan(&s.ID, &s.TaskID, &s.UserID, &s.Text, &s.Link, &attachmentJSON, &s.Status, &s.AttemptNo, &s.Version, &s.ReviewerID, &s.ReviewNote, &s.SubmittedAt, &s.ReviewedAt)
	if err == nil && len(attachmentJSON) > 0 {
		if decodeErr := json.Unmarshal(attachmentJSON, &s.AttachmentIDs); decodeErr != nil {
			return s, decodeErr
		}
	}
	return s, err
}

const submissionColumns = `submission_id,task_id,user_id,content_text,content_link,attachment_ids,status,attempt_no,version,COALESCE(reviewer_id,''),review_note,submitted_at,COALESCE(reviewed_at,0)`

func requireRole(ctx context.Context, q queryer, groupID, uid string, lock bool) (string, error) {
	if uid == "" || groupID == "" {
		return "", missing()
	}
	query := `SELECT gm.role FROM group_members gm JOIN im_groups g ON g.group_id=gm.group_id WHERE gm.group_id=? AND gm.user_id=? AND gm.status='active' AND g.status='active'`
	if lock {
		query += ` FOR UPDATE`
	}
	var role string
	err := q.QueryRowContext(ctx, query, groupID, uid).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", missing()
	}
	if err != nil {
		return "", unavailable(err)
	}
	return role, nil
}

func requireManager(role string) error {
	if role != "owner" && role != "admin" {
		return missing()
	}
	return nil
}

func (s *Service) db() error {
	if s == nil || s.DB == nil {
		return unavailable(errors.New("database is not configured"))
	}
	return nil
}

func checkIdempotent(ctx context.Context, tx *sql.Tx, actor, requestID, hash string) (json.RawMessage, bool, error) {
	var savedHash string
	var result []byte
	err := tx.QueryRowContext(ctx, `SELECT request_hash,result_json FROM task_events WHERE actor_uid=? AND request_id=?`, actor, requestID).Scan(&savedHash, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, unavailable(err)
	}
	if savedHash != hash {
		return nil, false, conflict("request_id already belongs to another action")
	}
	return json.RawMessage(result), true, nil
}

func saveEvent(ctx context.Context, tx *sql.Tx, eventID, taskID, submissionID, actor, action, requestID, hash string, payload, result any, targets []notifyTarget, now int64) error {
	p, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	r, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var sid any
	if submissionID != "" {
		sid = submissionID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_events (event_id,task_id,submission_id,actor_uid,action,request_id,request_hash,payload_json,result_json,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, eventID, taskID, sid, actor, action, requestID, hash, p, r, now)
	if err != nil {
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return conflict("request_id already belongs to another action")
		}
		return unavailable(err)
	}
	for _, target := range targets {
		_, err = tx.ExecContext(ctx, `INSERT INTO task_notification_outbox (event_id,target_type,target_id,status,attempts,available_at,created_at,updated_at) VALUES (?,?,?,'pending',0,?,?,?)`, eventID, target.kind, target.id, now, now, now)
		if err != nil {
			return unavailable(err)
		}
	}
	return nil
}

type mutation func(context.Context, *sql.Tx, *Task, string, int64) (any, string, []notifyTarget, error)

func (s *Service) mutate(ctx context.Context, actor, taskID, requestID, action string, payload any, fn mutation) (any, error) {
	if err := s.db(); err != nil {
		return nil, err
	}
	if err := validRequestID(requestID); err != nil {
		return nil, err
	}
	hash := requestHash(action, taskID, payload)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, unavailable(err)
	}
	defer tx.Rollback()
	t, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM group_tasks WHERE task_id=? FOR UPDATE`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, missing()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	role, err := requireRole(ctx, tx, t.GroupID, actor, true)
	if err != nil {
		return nil, err
	}
	if saved, ok, err := checkIdempotent(ctx, tx, actor, requestID, hash); err != nil {
		return nil, err
	} else if ok {
		return saved, nil
	}
	now := time.Now().UnixMilli()
	result, submissionID, targets, err := fn(ctx, tx, &t, role, now)
	if err != nil {
		return nil, err
	}
	eventID, err := newID("te_")
	if err != nil {
		return nil, unavailable(err)
	}
	if err := saveEvent(ctx, tx, eventID, taskID, submissionID, actor, action, requestID, hash, payload, result, targets, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, unavailable(err)
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, actor, groupID, requestID, title, description string, dueAt int64) (any, error) {
	if err := s.db(); err != nil {
		return nil, err
	}
	if err := validRequestID(requestID); err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	payload := struct {
		Title, Description string
		DueAt              int64
	}{title, description, dueAt}
	hash := requestHash("create", groupID, payload)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, unavailable(err)
	}
	defer tx.Rollback()
	role, err := requireRole(ctx, tx, groupID, actor, true)
	if err != nil {
		return nil, err
	}
	if err := requireManager(role); err != nil {
		return nil, err
	}
	if saved, ok, err := checkIdempotent(ctx, tx, actor, requestID, hash); err != nil {
		return nil, err
	} else if ok {
		return saved, nil
	}
	now := time.Now().UnixMilli()
	if err := validateTaskFields(title, description, dueAt, now); err != nil {
		return nil, err
	}
	id, err := newID("lt_")
	if err != nil {
		return nil, unavailable(err)
	}
	var due any
	if dueAt != 0 {
		due = dueAt
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO group_tasks (task_id,group_id,title,description,due_at,status,version,created_by,created_at,updated_at) VALUES (?,?,?,?,?,'draft',1,?,?,?)`, id, groupID, title, description, due, actor, now, now)
	if err != nil {
		return nil, unavailable(err)
	}
	t := Task{ID: id, GroupID: groupID, Title: title, Description: description, DueAt: dueAt, Status: "draft", Version: 1, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	eventID, err := newID("te_")
	if err != nil {
		return nil, unavailable(err)
	}
	if err := saveEvent(ctx, tx, eventID, id, "", actor, "create", requestID, hash, payload, t, nil, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, unavailable(err)
	}
	return t, nil
}

func (s *Service) UpdateDraft(ctx context.Context, actor, taskID, requestID string, expected int64, title, description string, dueAt int64) (any, error) {
	title = strings.TrimSpace(title)
	payload := struct {
		Expected           int64
		Title, Description string
		DueAt              int64
	}{expected, title, description, dueAt}
	return s.mutate(ctx, actor, taskID, requestID, "edit", payload, func(ctx context.Context, tx *sql.Tx, t *Task, role string, now int64) (any, string, []notifyTarget, error) {
		if err := requireManager(role); err != nil {
			return nil, "", nil, err
		}
		if err := validateTaskFields(title, description, dueAt, now); err != nil {
			return nil, "", nil, err
		}
		if t.Status != "draft" {
			return nil, "", nil, conflict("draft changed or already published")
		}
		if t.Version != expected {
			return nil, "", nil, versionConflict("draft version changed", t.Version)
		}
		var due any
		if dueAt != 0 {
			due = dueAt
		}
		_, err := tx.ExecContext(ctx, `UPDATE group_tasks SET title=?,description=?,due_at=?,version=version+1,updated_at=? WHERE task_id=?`, title, description, due, now, t.ID)
		if err != nil {
			return nil, "", nil, unavailable(err)
		}
		t.Title, t.Description, t.DueAt, t.Version, t.UpdatedAt = title, description, dueAt, t.Version+1, now
		return *t, "", nil, nil
	})
}

func (s *Service) Publish(ctx context.Context, actor, taskID, requestID string, expected int64) (any, error) {
	return s.mutate(ctx, actor, taskID, requestID, "publish", struct{ Expected int64 }{expected}, func(ctx context.Context, tx *sql.Tx, t *Task, role string, now int64) (any, string, []notifyTarget, error) {
		if err := requireManager(role); err != nil {
			return nil, "", nil, err
		}
		if t.Status != "draft" {
			return nil, "", nil, conflict("task is not an unchanged draft")
		}
		if t.Version != expected {
			return nil, "", nil, versionConflict("task version changed", t.Version)
		}
		if t.DueAt > 0 && now >= t.DueAt {
			return nil, "", nil, conflict("task deadline has passed before publication")
		}
		_, err := tx.ExecContext(ctx, `UPDATE group_tasks SET status='published',version=version+1,published_at=?,updated_at=? WHERE task_id=?`, now, now, t.ID)
		if err != nil {
			return nil, "", nil, unavailable(err)
		}
		t.Status = "published"
		t.Version++
		t.PublishedAt = now
		t.UpdatedAt = now
		return *t, "", []notifyTarget{{"group", t.GroupID}}, nil
	})
}

func (s *Service) Close(ctx context.Context, actor, taskID, requestID string, expected int64) (any, error) {
	return s.mutate(ctx, actor, taskID, requestID, "close", struct{ Expected int64 }{expected}, func(ctx context.Context, tx *sql.Tx, t *Task, role string, now int64) (any, string, []notifyTarget, error) {
		if err := requireManager(role); err != nil {
			return nil, "", nil, err
		}
		if t.Status != "published" {
			return nil, "", nil, conflict("task is not an unchanged published task")
		}
		if t.Version != expected {
			return nil, "", nil, versionConflict("task version changed", t.Version)
		}
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND status='submitted'`, t.ID).Scan(&pending); err != nil {
			return nil, "", nil, unavailable(err)
		}
		if pending > 0 {
			return nil, "", nil, conflict("review pending submissions before closing")
		}
		_, err := tx.ExecContext(ctx, `UPDATE group_tasks SET status='closed',version=version+1,closed_at=?,updated_at=? WHERE task_id=?`, now, now, t.ID)
		if err != nil {
			return nil, "", nil, unavailable(err)
		}
		t.Status = "closed"
		t.Version++
		t.ClosedAt = now
		t.UpdatedAt = now
		return *t, "", []notifyTarget{{"group", t.GroupID}}, nil
	})
}

func (s *Service) Get(ctx context.Context, actor, taskID string) (Detail, error) {
	if err := s.db(); err != nil {
		return Detail{}, err
	}
	t, err := scanTask(s.DB.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM group_tasks WHERE task_id=?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, missing()
	}
	if err != nil {
		return Detail{}, unavailable(err)
	}
	role, err := requireRole(ctx, s.DB, t.GroupID, actor, false)
	if err != nil {
		return Detail{}, err
	}
	if t.Status == "draft" && role == "member" {
		return Detail{}, missing()
	}
	d := Detail{Task: t, Role: role}
	if role == "member" {
		sub, err := scanSubmission(s.DB.QueryRowContext(ctx, `SELECT `+submissionColumns+` FROM task_submissions WHERE task_id=? AND user_id=?`, taskID, actor))
		if err == nil {
			d.MySubmission = &sub
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Detail{}, unavailable(err)
		}
	}
	return d, nil
}

func (s *Service) List(ctx context.Context, actor, groupID, cursor string, limit int) (TaskList, error) {
	if err := s.db(); err != nil {
		return TaskList{}, err
	}
	role, err := requireRole(ctx, s.DB, groupID, actor, false)
	if err != nil {
		return TaskList{}, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	var beforeAt int64
	var beforeID string
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return TaskList{}, bad("invalid cursor")
		}
		parts := strings.SplitN(string(b), ":", 2)
		if len(parts) != 2 {
			return TaskList{}, bad("invalid cursor")
		}
		beforeAt, e = strconv.ParseInt(parts[0], 10, 64)
		if e != nil {
			return TaskList{}, bad("invalid cursor")
		}
		beforeID = parts[1]
	}
	query := `SELECT ` + taskColumns + ` FROM group_tasks WHERE group_id=?`
	args := []any{groupID}
	if role == "member" {
		query += ` AND status<>'draft'`
	}
	if cursor != "" {
		query += ` AND (created_at<? OR (created_at=? AND task_id<?))`
		args = append(args, beforeAt, beforeAt, beforeID)
	}
	query += ` ORDER BY created_at DESC,task_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return TaskList{}, unavailable(err)
	}
	defer rows.Close()
	out := TaskList{Tasks: make([]Task, 0, limit)}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return TaskList{}, unavailable(e)
		}
		out.Tasks = append(out.Tasks, t)
	}
	if err := rows.Err(); err != nil {
		return TaskList{}, unavailable(err)
	}
	if len(out.Tasks) > limit {
		out.Tasks = out.Tasks[:limit]
		last := out.Tasks[len(out.Tasks)-1]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%s", last.CreatedAt, last.ID)))
	}
	return out, nil
}
