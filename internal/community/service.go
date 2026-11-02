package community

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Service struct{ DB *sql.DB }
type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func role(ctx context.Context, q queryer, gid, uid string, lock bool) (string, error) {
	query := `SELECT m.role FROM group_members m JOIN im_groups g ON g.group_id=m.group_id WHERE m.group_id=? AND m.user_id=? AND m.status='active' AND g.status='active'`
	if lock {
		query += ` FOR UPDATE`
	}
	var r string
	err := q.QueryRowContext(ctx, query, gid, uid).Scan(&r)
	if errors.Is(err, sql.ErrNoRows) {
		return "", denied()
	}
	return r, err
}
func (s *Service) Register(ctx context.Context, username, password string) (any, error) {
	username = strings.TrimSpace(username)
	if !bounded(username, 3, 32) || len(password) < 8 || len(password) > 72 {
		return nil, invalid("用户名需3–32字，密码需8–72字节")
	}
	if strings.EqualFold(username, "task_notifier") {
		return nil, invalid("reserved username")
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	uid := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO users(user_id,username,password,status,created_at,updated_at) VALUES(?,?,?,1,?,?)`, uid, username, string(digest), now, now)
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		return nil, conflict("用户名已存在")
	}
	return map[string]string{"user_id": uid, "username": username}, err
}

// Actor-row lock serializes retries before checking request_id. Result and
// domain mutations commit atomically; same key with a new payload is rejected.
func (s *Service) mutate(ctx context.Context, actor, key, operation string, input any, fn func(*sql.Tx, int64) (any, error)) (any, error) {
	if !bounded(key, 1, 128) {
		return nil, invalid("request_id is required (max 128 characters)")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var uid string
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM users WHERE user_id=? AND status=1 FOR UPDATE`, actor).Scan(&uid); err != nil {
		return nil, denied()
	}
	fingerprint := hash([]any{operation, input})
	var old string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,result_json FROM community_requests WHERE actor_id=? AND request_id=?`, actor, key).Scan(&old, &raw)
	if err == nil {
		if old != fingerprint {
			return nil, conflict("request_id已用于其他内容")
		}
		return json.RawMessage(raw), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	now := time.Now().UnixMilli()
	result, err := fn(tx, now)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO community_requests(actor_id,request_id,request_hash,result_json,created_at) VALUES(?,?,?,?,?)`, actor, key, fingerprint, raw, now)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func lockGroup(ctx context.Context, tx *sql.Tx, gid string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT group_id FROM im_groups WHERE group_id=? AND status='active' FOR UPDATE`, gid).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return denied()
	}
	return err
}
func emit(ctx context.Context, tx *sql.Tx, actor, gid, kind, object, recipient string, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO community_event_outbox(event_id,group_id,actor_id,kind,object_id,recipient_id,available_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, uuid.NewString(), gid, actor, kind, object, recipient, now, now)
	return err
}
func (s *Service) Create(ctx context.Context, actor string, in Input) (any, error) {
	if !bounded(in.Name, 1, 80) || !bounded(in.Description, 0, 1000) {
		return nil, invalid("社区名称1–80字，简介最多1000字")
	}
	return s.mutate(ctx, actor, in.RequestID, "create", in, func(tx *sql.Tx, now int64) (any, error) {
		gid := "c-" + uuid.NewString()
		if _, e := tx.ExecContext(ctx, `INSERT INTO im_groups(group_id,name,owner_id,status,created_at,updated_at) VALUES(?,?,?,'active',?,?)`, gid, in.Name, actor, now, now); e != nil {
			return nil, e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO community_profiles(group_id,description) VALUES(?,?)`, gid, in.Description); e != nil {
			return nil, e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO group_members(group_id,user_id,role,status,joined_at) VALUES(?,?,'owner','active',?)`, gid, actor, now); e != nil {
			return nil, e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO conversation_members(conversation_id,user_id,read_seq,acked_seq,joined_at) VALUES(?,?,0,0,?)`, "group:"+gid, actor, now); e != nil {
			return nil, e
		}
		return Community{ID: gid, Name: in.Name, Description: in.Description, Owner: actor, Role: "owner", Members: 1}, nil
	})
}
func (s *Service) List(ctx context.Context, actor string, mine bool) ([]Community, error) {
	clause := `g.status='active' AND (COALESCE(p.discoverable,1)=1 OR m.status='active')`
	if mine {
		clause = `g.status='active' AND m.status='active'`
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT g.group_id,g.name,COALESCE(p.description,''),g.owner_id,IF(m.status='active',m.role,''),(SELECT COUNT(*) FROM group_members x WHERE x.group_id=g.group_id AND x.status='active') FROM im_groups g LEFT JOIN community_profiles p ON p.group_id=g.group_id LEFT JOIN group_members m ON m.group_id=g.group_id AND m.user_id=? WHERE `+clause+` ORDER BY g.created_at DESC,g.group_id LIMIT 100`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Community{}
	for rows.Next() {
		var c Community
		if e := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Owner, &c.Role, &c.Members); e != nil {
			return nil, e
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Service) Membership(ctx context.Context, actor, gid, action string, in Input) (any, error) {
	return s.mutate(ctx, actor, in.RequestID, action+":"+gid, in, func(tx *sql.Tx, now int64) (any, error) {
		if err := lockGroup(ctx, tx, gid); err != nil {
			return nil, err
		}
		switch action {
		case "join":
			var discoverable int
			err := tx.QueryRowContext(ctx, `SELECT discoverable FROM community_profiles WHERE group_id=?`, gid).Scan(&discoverable)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err == nil && discoverable != 1 {
				return nil, denied()
			}
			var oldStatus string
			err = tx.QueryRowContext(ctx, `SELECT status FROM group_members WHERE group_id=? AND user_id=?`, gid, actor).Scan(&oldStatus)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if oldStatus == "removed" {
				return nil, denied()
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO group_members(group_id,user_id,role,status,joined_at) VALUES(?,?,'member','active',?) ON DUPLICATE KEY UPDATE status='active'`, gid, actor, now); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO conversation_members(conversation_id,user_id,read_seq,acked_seq,joined_at) VALUES(?,?,0,0,?) ON DUPLICATE KEY UPDATE joined_at=joined_at`, "group:"+gid, actor, now); err != nil {
				return nil, err
			}
		case "leave", "remove", "transfer":
			r, err := role(ctx, tx, gid, actor, true)
			if err != nil {
				return nil, err
			}
			target := actor
			status := "left"
			if action != "leave" {
				if !manager(r) || in.UserID == "" || in.UserID == actor {
					return nil, denied()
				}
				target = in.UserID
			}
			tr, err := role(ctx, tx, gid, target, true)
			if err != nil {
				return nil, err
			}
			if action == "transfer" {
				if r != "owner" {
					return nil, denied()
				}
				if _, err = tx.ExecContext(ctx, `UPDATE group_members SET role=CASE WHEN user_id=? THEN 'owner' ELSE 'member' END WHERE group_id=? AND user_id IN (?,?)`, target, gid, actor, target); err != nil {
					return nil, err
				}
				if _, err = tx.ExecContext(ctx, `UPDATE im_groups SET owner_id=?,updated_at=? WHERE group_id=?`, target, now, gid); err != nil {
					return nil, err
				}
			} else {
				if tr == "owner" {
					return nil, conflict("创建者需先转让社区")
				}
				if action == "remove" {
					status = "removed"
					if r != "owner" && tr != "member" {
						return nil, denied()
					}
				}
				if _, err = tx.ExecContext(ctx, `UPDATE group_members SET status=? WHERE group_id=? AND user_id=?`, status, gid, target); err != nil {
					return nil, err
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM conversation_members WHERE conversation_id=? AND user_id=?`, "group:"+gid, target); err != nil {
					return nil, err
				}
			}
		default:
			return nil, invalid("invalid membership action")
		}
		return map[string]any{"group_id": gid, "action": action, "ok": true}, nil
	})
}

const postCols = `p.post_id,p.group_id,p.author_id,p.kind,p.title,p.body,p.status,p.pinned,p.featured,p.version,p.created_at,p.updated_at,(SELECT COUNT(*) FROM community_reactions l WHERE l.post_id=p.post_id),EXISTS(SELECT 1 FROM community_reactions l WHERE l.post_id=p.post_id AND l.user_id=?),(SELECT COUNT(*) FROM community_comments c WHERE c.post_id=p.post_id)`

type scanner interface{ Scan(...any) error }

func scanPost(row scanner) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.GroupID, &p.AuthorID, &p.Kind, &p.Title, &p.Body, &p.Status, &p.Pinned, &p.Featured, &p.Version, &p.CreatedAt, &p.UpdatedAt, &p.Likes, &p.Liked, &p.Comments)
	p.Attachments = []Attachment{}
	return p, err
}
func validPost(in Input) error {
	if !bounded(in.Title, 1, 160) || !bounded(in.Body, 1, 10000) {
		return invalid("标题1–160字，正文1–10000字")
	}
	switch in.Kind {
	case "question", "note", "resource", "announcement":
	default:
		return invalid("invalid post kind")
	}
	if len(in.AttachmentIDs) > 8 {
		return invalid("最多8个附件")
	}
	return nil
}
func (s *Service) CreatePost(ctx context.Context, actor, gid string, in Input) (any, error) {
	if err := validPost(in); err != nil {
		return nil, err
	}
	return s.mutate(ctx, actor, in.RequestID, "post:"+gid, in, func(tx *sql.Tx, now int64) (any, error) {
		if err := lockGroup(ctx, tx, gid); err != nil {
			return nil, err
		}
		r, err := role(ctx, tx, gid, actor, true)
		if err != nil {
			return nil, err
		}
		if in.Kind == "announcement" && !manager(r) {
			return nil, denied()
		}
		pid := uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO community_posts(post_id,group_id,author_id,kind,title,body,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, pid, gid, actor, in.Kind, in.Title, in.Body, now, now)
		if err != nil {
			return nil, err
		}
		for _, id := range in.AttachmentIDs {
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM attachments WHERE attachment_id=? AND group_id=? AND uploader_id=? AND status='ready'`, id, gid, actor).Scan(&count); err != nil {
				return nil, err
			}
			if count != 1 {
				return nil, invalid("附件不存在、非本人上传或属于其他社区")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO community_post_attachments(post_id,attachment_id) VALUES(?,?)`, pid, id); err != nil {
				return nil, err
			}
		}
		if err = emit(ctx, tx, actor, gid, "post_created", pid, "", now); err != nil {
			return nil, err
		}
		return map[string]any{"post_id": pid, "version": 1}, nil
	})
}

type cursor struct {
	Pinned bool   `json:"p"`
	At     int64  `json:"t"`
	ID     string `json:"i"`
}

func encodeCursor(p Post) string {
	b, _ := json.Marshal(cursor{p.Pinned, p.CreatedAt, p.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(v string) (cursor, error) {
	var c cursor
	b, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(b) > 256 {
		return c, invalid("invalid cursor")
	}
	if e = json.Unmarshal(b, &c); e != nil || c.At <= 0 || c.ID == "" {
		return c, invalid("invalid cursor")
	}
	return c, nil
}
func (s *Service) Posts(ctx context.Context, actor, gid, after, kind, filter string) (any, error) {
	r, err := role(ctx, s.DB, gid, actor, false)
	if err != nil {
		return nil, err
	}
	if filter == "hidden" && !manager(r) {
		return nil, denied()
	}
	query := `SELECT ` + postCols + ` FROM community_posts p WHERE p.group_id=? AND p.status='published'`
	args := []any{actor, gid}
	if filter == "hidden" {
		query = strings.Replace(query, "p.status='published'", "p.status='hidden'", 1)
	}
	if kind != "" {
		query += ` AND p.kind=?`
		args = append(args, kind)
	}
	if filter == "featured" {
		query += ` AND p.featured=1`
	}
	if filter == "owner" {
		query += ` AND p.author_id=(SELECT owner_id FROM im_groups WHERE group_id=p.group_id)`
	}
	if after != "" {
		c, e := decodeCursor(after)
		if e != nil {
			return nil, e
		}
		query += ` AND (p.pinned<? OR (p.pinned=? AND (p.created_at<? OR (p.created_at=? AND p.post_id<?))))`
		args = append(args, c.Pinned, c.Pinned, c.At, c.At, c.ID)
	}
	query += ` ORDER BY p.pinned DESC,p.created_at DESC,p.post_id DESC LIMIT 21`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Post{}
	for rows.Next() {
		p, e := scanPost(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	next := ""
	if len(items) > 20 {
		items = items[:20]
		next = encodeCursor(items[19])
	}
	return map[string]any{"posts": items, "next_cursor": next}, nil
}
func (s *Service) Post(ctx context.Context, actor, pid string) (any, error) {
	p, err := scanPost(s.DB.QueryRowContext(ctx, `SELECT `+postCols+` FROM community_posts p WHERE p.post_id=? AND p.status<>'deleted'`, actor, pid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, denied()
	}
	if err != nil {
		return nil, err
	}
	r, err := role(ctx, s.DB, p.GroupID, actor, false)
	if err != nil {
		return nil, err
	}
	if p.Status == "hidden" && !manager(r) {
		return nil, denied()
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.attachment_id,a.file_name,a.file_size FROM community_post_attachments x JOIN attachments a ON a.attachment_id=x.attachment_id WHERE x.post_id=? AND a.status='ready'`, pid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a Attachment
		if err = rows.Scan(&a.ID, &a.Name, &a.Size); err != nil {
			rows.Close()
			return nil, err
		}
		p.Attachments = append(p.Attachments, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (s *Service) EditPost(ctx context.Context, actor, pid string, in Input) (any, error) {
	return s.mutate(ctx, actor, in.RequestID, "edit:"+pid, in, func(tx *sql.Tx, now int64) (any, error) {
		var gid, author, kind, status string
		var version int64
		err := tx.QueryRowContext(ctx, `SELECT group_id,author_id,kind,status,version FROM community_posts WHERE post_id=? FOR UPDATE`, pid).Scan(&gid, &author, &kind, &status, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, denied()
		}
		if err != nil {
			return nil, err
		}
		r, err := role(ctx, tx, gid, actor, true)
		if err != nil {
			return nil, err
		}
		if status == "deleted" || (!manager(r) && actor != author) {
			return nil, denied()
		}
		if version != in.ExpectedVersion {
			return nil, conflict("版本已变化，请刷新")
		}
		if in.Status == "hidden" || in.Status == "published" {
			if !manager(r) {
				return nil, denied()
			}
			status = in.Status
		} else if in.Status == "deleted" {
			status = "deleted"
		} else if in.Status != "" {
			return nil, invalid("invalid status")
		}
		if in.Title == "" {
			return nil, invalid("标题不能为空")
		}
		in.Kind = kind
		if err = validPost(in); err != nil {
			return nil, err
		}
		if !manager(r) {
			var pinned, featured bool
			if err = tx.QueryRowContext(ctx, `SELECT pinned,featured FROM community_posts WHERE post_id=?`, pid).Scan(&pinned, &featured); err != nil {
				return nil, err
			}
			in.Pinned = pinned
			in.Featured = featured
		}
		result, err := tx.ExecContext(ctx, `UPDATE community_posts SET title=?,body=?,status=?,pinned=?,featured=?,version=version+1,updated_at=? WHERE post_id=? AND version=?`, in.Title, in.Body, status, in.Pinned, in.Featured, now, pid, in.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return nil, conflict("版本已变化，请刷新")
		}
		return map[string]any{"post_id": pid, "version": version + 1, "status": status}, nil
	})
}
func (s *Service) Comment(ctx context.Context, actor, pid string, in Input) (any, error) {
	if !bounded(in.Body, 1, 2000) {
		return nil, invalid("评论1–2000字")
	}
	return s.mutate(ctx, actor, in.RequestID, "comment:"+pid, in, func(tx *sql.Tx, now int64) (any, error) {
		var gid, author string
		if err := tx.QueryRowContext(ctx, `SELECT group_id,author_id FROM community_posts WHERE post_id=? AND status='published' FOR UPDATE`, pid).Scan(&gid, &author); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, denied()
			}
			return nil, err
		}
		if _, err := role(ctx, tx, gid, actor, true); err != nil {
			return nil, err
		}
		recipient := author
		var parent any
		if in.ParentID != "" {
			var root sql.NullString
			if err := tx.QueryRowContext(ctx, `SELECT author_id,parent_id FROM community_comments WHERE comment_id=? AND post_id=?`, in.ParentID, pid).Scan(&recipient, &root); err != nil {
				return nil, invalid("回复目标不存在")
			}
			if root.Valid {
				return nil, invalid("只支持一层回复")
			}
			parent = in.ParentID
		}
		id := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO community_comments(comment_id,post_id,author_id,parent_id,body,created_at) VALUES(?,?,?,?,?,?)`, id, pid, actor, parent, in.Body, now); err != nil {
			return nil, err
		}
		if recipient != actor {
			if err := emit(ctx, tx, actor, gid, "comment_created", pid, recipient, now); err != nil {
				return nil, err
			}
		}
		return Comment{ID: id, Author: actor, Parent: in.ParentID, Body: in.Body, CreatedAt: now}, nil
	})
}
func (s *Service) Comments(ctx context.Context, actor, pid string) (any, error) {
	if _, err := s.Post(ctx, actor, pid); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT comment_id,author_id,COALESCE(parent_id,''),body,created_at FROM community_comments WHERE post_id=? ORDER BY created_at,comment_id LIMIT 200`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Comment{}
	for rows.Next() {
		var c Comment
		if err = rows.Scan(&c.ID, &c.Author, &c.Parent, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return map[string]any{"comments": items}, rows.Err()
}
func (s *Service) Like(ctx context.Context, actor, pid string, liked bool) (any, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var gid string
	err = tx.QueryRowContext(ctx, `SELECT group_id FROM community_posts WHERE post_id=? AND status='published' FOR UPDATE`, pid).Scan(&gid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, denied()
	}
	if err != nil {
		return nil, err
	}
	if _, err = role(ctx, tx, gid, actor, true); err != nil {
		return nil, err
	}
	if liked {
		_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO community_reactions(post_id,user_id,created_at) VALUES(?,?,?)`, pid, actor, time.Now().UnixMilli())
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM community_reactions WHERE post_id=? AND user_id=?`, pid, actor)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]bool{"liked": liked}, nil
}
func (s *Service) Notifications(ctx context.Context, actor string, after int64) (any, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT n.id,n.event_id,n.group_id,n.kind,n.object_id,n.created_at,COALESCE(n.read_at,0) FROM community_notifications n JOIN group_members m ON m.group_id=n.group_id AND m.user_id=n.recipient_id AND m.status='active' WHERE n.recipient_id=? AND n.id>? ORDER BY n.id LIMIT 100`, actor, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	next := after
	for rows.Next() {
		var id, at, read int64
		var event, gid, kind, object string
		if err = rows.Scan(&id, &event, &gid, &kind, &object, &at, &read); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "event_id": event, "group_id": gid, "kind": kind, "object_id": object, "created_at": at, "read_at": read})
		next = id
	}
	return map[string]any{"notifications": items, "next_cursor": next}, rows.Err()
}
func (s *Service) ReadNotification(ctx context.Context, actor, id string) (any, error) {
	_, err := s.DB.ExecContext(ctx, `UPDATE community_notifications SET read_at=COALESCE(read_at,?) WHERE id=? AND recipient_id=?`, time.Now().UnixMilli(), id, actor)
	return map[string]bool{"ok": true}, err
}

// String is deliberately content-free, suitable for operational logs.
func (s *Service) String() string { return fmt.Sprintf("community.Service(db=%t)", s.DB != nil) }
