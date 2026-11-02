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
)

func (s *Service) Submit(ctx context.Context, actor, taskID, requestID, text, link string, expected int64, attachmentArgs ...[]string) (any, error) {
	text = strings.TrimSpace(text)
	link = strings.TrimSpace(link)
	var attachmentIDs []string
	if len(attachmentArgs) > 0 {
		attachmentIDs = attachmentArgs[0]
	}
	if err := validateSubmission(text, link, attachmentIDs); err != nil {
		return nil, err
	}
	attachmentJSON, _ := json.Marshal(attachmentIDs)
	payload := struct {
		Text, Link  string
		Expected    int64
		Attachments []string
	}{text, link, expected, attachmentIDs}
	return s.mutate(ctx, actor, taskID, requestID, "submit", payload, func(ctx context.Context, tx *sql.Tx, t *Task, role string, now int64) (any, string, []notifyTarget, error) {
		if role != "member" {
			return nil, "", nil, missing()
		}
		if t.Status != "published" {
			return nil, "", nil, conflict("task is not accepting submissions")
		}
		if t.DueAt > 0 && now >= t.DueAt {
			return nil, "", nil, conflict("task deadline has passed")
		}
		if err := validateTaskAttachments(ctx, tx, t.GroupID, actor, attachmentIDs); err != nil {
			return nil, "", nil, err
		}
		q := `SELECT ` + submissionColumns + ` FROM task_submissions WHERE task_id=? AND user_id=? FOR UPDATE`
		sub, err := scanSubmission(tx.QueryRowContext(ctx, q, taskID, actor))
		if errors.Is(err, sql.ErrNoRows) {
			if expected != 0 {
				return nil, "", nil, versionConflict("submission version changed", 0)
			}
			id, e := newID("ts_")
			if e != nil {
				return nil, "", nil, unavailable(e)
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO task_submissions (submission_id,task_id,user_id,content_text,content_link,attachment_ids,status,attempt_no,version,submitted_at) VALUES (?,?,?,?,?,?, 'submitted',1,1,?)`, id, taskID, actor, text, link, attachmentJSON, now)
			if e != nil {
				return nil, "", nil, unavailable(e)
			}
			sub = Submission{ID: id, TaskID: taskID, UserID: actor, Text: text, Link: link, AttachmentIDs: attachmentIDs, Status: "submitted", AttemptNo: 1, Version: 1, SubmittedAt: now}
		} else if err != nil {
			return nil, "", nil, unavailable(err)
		} else {
			if sub.Version != expected {
				return nil, "", nil, versionConflict("submission version changed", sub.Version)
			}
			if sub.Status != "returned" {
				return nil, "", nil, conflict("submission is not an unchanged returned submission")
			}
			_, err = tx.ExecContext(ctx, `UPDATE task_submissions SET content_text=?,content_link=?,attachment_ids=?,status='submitted',attempt_no=attempt_no+1,version=version+1,reviewer_id=NULL,review_note='',reviewed_at=NULL,submitted_at=? WHERE submission_id=?`, text, link, attachmentJSON, now, sub.ID)
			if err != nil {
				return nil, "", nil, unavailable(err)
			}
			sub.Text = text
			sub.Link = link
			sub.AttachmentIDs = attachmentIDs
			sub.Status = "submitted"
			sub.AttemptNo++
			sub.Version++
			sub.ReviewerID = ""
			sub.ReviewNote = ""
			sub.ReviewedAt = 0
			sub.SubmittedAt = now
		}
		rows, err := tx.QueryContext(ctx, `SELECT user_id FROM group_members WHERE group_id=? AND status='active' AND role IN ('owner','admin') ORDER BY user_id`, t.GroupID)
		if err != nil {
			return nil, "", nil, unavailable(err)
		}
		var targets []notifyTarget
		for rows.Next() {
			var uid string
			if e := rows.Scan(&uid); e != nil {
				rows.Close()
				return nil, "", nil, unavailable(e)
			}
			targets = append(targets, notifyTarget{"user", uid})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", nil, unavailable(err)
		}
		return sub, sub.ID, targets, nil
	})
}

func validateTaskAttachments(ctx context.Context, tx *sql.Tx, groupID, actor string, ids []string) error {
	for _, id := range ids {
		var attachmentGroup string
		err := tx.QueryRowContext(ctx, `SELECT group_id FROM attachments WHERE attachment_id=? AND uploader_id=? AND status='ready'`, id, actor).Scan(&attachmentGroup)
		if errors.Is(err, sql.ErrNoRows) {
			return bad("附件不存在、未就绪或不属于当前提交人")
		}
		if err != nil {
			return unavailable(err)
		}
		if attachmentGroup != groupID {
			return bad("附件不属于当前学习群")
		}
	}
	return nil
}

func (s *Service) Review(ctx context.Context, actor, submissionID, requestID, decision, reason string, expected int64) (any, error) {
	if err := s.db(); err != nil {
		return nil, err
	}
	decision = strings.TrimSpace(decision)
	reason = strings.TrimSpace(reason)
	if decision != "approve" && decision != "return" {
		return nil, bad("decision must be approve or return")
	}
	if decision == "return" && (len([]rune(reason)) < 1 || len([]rune(reason)) > 1000) {
		return nil, bad("return reason must be 1-1000 characters")
	}
	if decision == "approve" {
		reason = ""
	}
	var taskID string
	err := s.DB.QueryRowContext(ctx, `SELECT task_id FROM task_submissions WHERE submission_id=?`, submissionID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, missing()
	}
	if err != nil {
		return nil, unavailable(err)
	}
	payload := struct {
		SubmissionID, Decision, Reason string
		Expected                       int64
	}{submissionID, decision, reason, expected}
	return s.mutate(ctx, actor, taskID, requestID, "review", payload, func(ctx context.Context, tx *sql.Tx, t *Task, role string, now int64) (any, string, []notifyTarget, error) {
		if err := requireManager(role); err != nil {
			return nil, "", nil, err
		}
		if t.Status != "published" {
			return nil, "", nil, conflict("task is closed")
		}
		sub, e := scanSubmission(tx.QueryRowContext(ctx, `SELECT `+submissionColumns+` FROM task_submissions WHERE submission_id=? AND task_id=? FOR UPDATE`, submissionID, taskID))
		if errors.Is(e, sql.ErrNoRows) {
			return nil, "", nil, missing()
		}
		if e != nil {
			return nil, "", nil, unavailable(e)
		}
		if sub.Version != expected {
			return nil, "", nil, versionConflict("submission version changed", sub.Version)
		}
		if sub.Status != "submitted" {
			return nil, "", nil, conflict("submission already reviewed or changed")
		}
		status := "approved"
		if decision == "return" {
			status = "returned"
		}
		_, e = tx.ExecContext(ctx, `UPDATE task_submissions SET status=?,version=version+1,reviewer_id=?,review_note=?,reviewed_at=? WHERE submission_id=?`, status, actor, reason, now, sub.ID)
		if e != nil {
			return nil, "", nil, unavailable(e)
		}
		sub.Status = status
		sub.Version++
		sub.ReviewerID = actor
		sub.ReviewNote = reason
		sub.ReviewedAt = now
		return sub, sub.ID, []notifyTarget{{"user", sub.UserID}}, nil
	})
}

func (s *Service) ListSubmissions(ctx context.Context, actor, taskID, cursor string, limit int) (SubmissionList, error) {
	if err := s.db(); err != nil {
		return SubmissionList{}, err
	}
	d, err := s.Get(ctx, actor, taskID)
	if err != nil {
		return SubmissionList{}, err
	}
	if err := requireManager(d.Role); err != nil {
		return SubmissionList{}, err
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
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return SubmissionList{}, bad("invalid cursor")
		}
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return SubmissionList{}, bad("invalid cursor")
		}
		beforeAt, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || beforeAt <= 0 {
			return SubmissionList{}, bad("invalid cursor")
		}
		beforeID = parts[1]
	}
	query := `SELECT ` + submissionColumns + ` FROM task_submissions WHERE task_id=?`
	args := []any{taskID}
	if cursor != "" {
		query += ` AND (submitted_at<? OR (submitted_at=? AND submission_id<?))`
		args = append(args, beforeAt, beforeAt, beforeID)
	}
	query += ` ORDER BY submitted_at DESC,submission_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return SubmissionList{}, unavailable(err)
	}
	defer rows.Close()
	out := SubmissionList{Submissions: make([]Submission, 0, limit)}
	for rows.Next() {
		v, e := scanSubmission(rows)
		if e != nil {
			return SubmissionList{}, unavailable(e)
		}
		out.Submissions = append(out.Submissions, v)
	}
	if err := rows.Err(); err != nil {
		return SubmissionList{}, unavailable(err)
	}
	if len(out.Submissions) > limit {
		out.Submissions = out.Submissions[:limit]
		last := out.Submissions[len(out.Submissions)-1]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%s", last.SubmittedAt, last.ID)))
	}
	return out, nil
}

func (s *Service) Progress(ctx context.Context, actor, taskID string) (Progress, error) {
	if err := s.db(); err != nil {
		return Progress{}, err
	}
	d, err := s.Get(ctx, actor, taskID)
	if err != nil {
		return Progress{}, err
	}
	if err := requireManager(d.Role); err != nil {
		return Progress{}, err
	}
	p := Progress{TaskID: taskID}
	err = s.DB.QueryRowContext(ctx, `
SELECT COUNT(*),
 COALESCE(SUM(CASE WHEN ts.submission_id IS NULL THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN ts.status='submitted' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN ts.status='returned' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN ts.status='approved' THEN 1 ELSE 0 END),0)
FROM group_members gm LEFT JOIN task_submissions ts ON ts.task_id=? AND ts.user_id=gm.user_id
WHERE gm.group_id=? AND gm.status='active' AND gm.role='member'`, taskID, d.Task.GroupID).Scan(&p.Total, &p.NotSubmitted, &p.Submitted, &p.Returned, &p.Approved)
	if err != nil {
		return Progress{}, unavailable(err)
	}
	return p, nil
}
