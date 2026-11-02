package attachment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MaxUploadBytes int64 = 25 << 20

type Store struct {
	DB      *sql.DB
	Dir     string
	Objects ObjectStore
}

type Metadata struct {
	ID       string `json:"attachment_id"`
	GroupID  string `json:"group_id"`
	Name     string `json:"file_name"`
	MIME     string `json:"mime_type"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Uploaded string `json:"uploaded_by"`
}

type MessageBody struct {
	Kind         string `json:"kind"`
	AttachmentID string `json:"attachment_id"`
	Name         string `json:"file_name"`
	MIME         string `json:"mime_type"`
	Size         int64  `json:"size"`
}

func (s *Store) Upload(ctx context.Context, userID, groupID, filename string, src io.Reader) (Metadata, error) {
	if s == nil || s.DB == nil || s.Dir == "" {
		return Metadata{}, errors.New("attachment storage is not configured")
	}
	if userID == "" || groupID == "" {
		return Metadata{}, errors.New("group and user are required")
	}
	var active int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_members WHERE group_id=? AND user_id=? AND status='active'`, groupID, userID).Scan(&active); err != nil {
		return Metadata{}, err
	}
	if active == 0 {
		return Metadata{}, ErrForbidden
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || len(filename) > 200 || strings.IndexByte(filename, 0) >= 0 {
		return Metadata{}, errors.New("invalid file name")
	}
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return Metadata{}, err
	}
	id := hex.EncodeToString(randomID[:])
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return Metadata{}, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".upload-*")
	if err != nil {
		return Metadata{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hash := sha256.New()
	limited := io.LimitReader(src, MaxUploadBytes+1)
	size, err := io.Copy(io.MultiWriter(tmp, hash), limited)
	if err != nil {
		_ = tmp.Close()
		return Metadata{}, err
	}
	if size == 0 || size > MaxUploadBytes {
		_ = tmp.Close()
		return Metadata{}, fmt.Errorf("file must be between 1 byte and %d bytes", MaxUploadBytes)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		return Metadata{}, err
	}
	var sniff [512]byte
	n, _ := tmp.Read(sniff[:])
	mimeType := http.DetectContentType(sniff[:n])
	if !allowed(filename, mimeType) {
		_ = tmp.Close()
		return Metadata{}, errors.New("unsupported file type")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Metadata{}, err
	}
	if err := tmp.Close(); err != nil {
		return Metadata{}, err
	}
	finalName := filepath.Join(s.Dir, id)
	if err := os.Rename(tmpName, finalName); err != nil {
		return Metadata{}, err
	}
	storageKey := id
	if s.Objects != nil {
		object, err := os.Open(finalName)
		if err != nil {
			return Metadata{}, err
		}
		storageKey = "s3:community/" + groupID + "/" + id
		err = s.Objects.Put(ctx, strings.TrimPrefix(storageKey, "s3:"), object, size, mimeType)
		object.Close()
		os.Remove(finalName)
		if err != nil {
			return Metadata{}, err
		}
	}
	meta := Metadata{ID: id, GroupID: groupID, Name: filename, MIME: mimeType, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), Uploaded: userID}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO attachments (attachment_id,group_id,uploader_id,file_name,mime_type,file_size,sha256,storage_key,status,created_at) VALUES (?,?,?,?,?,?,?,?, 'ready', UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000)`, meta.ID, meta.GroupID, meta.Uploaded, meta.Name, meta.MIME, meta.Size, meta.SHA256, storageKey)
	if err != nil {
		_ = os.Remove(finalName)
		if s.Objects != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.Objects.Delete(cleanup, strings.TrimPrefix(storageKey, "s3:"))
		}
		return Metadata{}, err
	}
	return meta, nil
}

func allowed(name, detected string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if strings.HasPrefix(detected, "image/") {
		return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp"
	}
	if strings.HasPrefix(detected, "video/") {
		return ext == ".mp4" || ext == ".webm" || ext == ".mov"
	}
	switch ext {
	case ".pdf":
		return detected == "application/pdf"
	case ".txt", ".md", ".csv":
		return strings.HasPrefix(detected, "text/")
	case ".docx", ".xlsx", ".pptx":
		return detected == "application/zip"
	default:
		return false
	}
}

var ErrForbidden = errors.New("group membership required")

func (s *Store) AuthorizedFile(ctx context.Context, userID, id string) (Metadata, io.ReadCloser, error) {
	var meta Metadata
	var key string
	err := s.DB.QueryRowContext(ctx, `SELECT a.attachment_id,a.group_id,a.uploader_id,a.file_name,a.mime_type,a.file_size,a.sha256,a.storage_key FROM attachments a JOIN group_members gm ON gm.group_id=a.group_id AND gm.user_id=? AND gm.status='active' WHERE a.attachment_id=? AND a.status='ready'`, userID, id).Scan(&meta.ID, &meta.GroupID, &meta.Uploaded, &meta.Name, &meta.MIME, &meta.Size, &meta.SHA256, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return Metadata{}, nil, ErrForbidden
	}
	if err != nil {
		return Metadata{}, nil, err
	}
	if strings.HasPrefix(key, "s3:") {
		if s.Objects == nil {
			return Metadata{}, nil, errors.New("object store not configured")
		}
		file, err := s.Objects.Open(ctx, strings.TrimPrefix(key, "s3:"))
		return meta, file, err
	}
	if filepath.Base(key) != key || key == "" {
		return Metadata{}, nil, errors.New("invalid storage key")
	}
	f, err := os.Open(filepath.Join(s.Dir, key))
	return meta, f, err
}

// CanonicalizeMessage validates an attachment reference and replaces all
// client-supplied display metadata with the database record.
func (s *Store) CanonicalizeMessage(ctx context.Context, sender, groupID, body string) (string, error) {
	var candidate MessageBody
	if err := json.Unmarshal([]byte(body), &candidate); err != nil || candidate.Kind != "attachment" {
		return body, nil
	}
	var meta Metadata
	err := s.DB.QueryRowContext(ctx, `SELECT attachment_id,group_id,uploader_id,file_name,mime_type,file_size,sha256 FROM attachments WHERE attachment_id=? AND status='ready'`, candidate.AttachmentID).Scan(&meta.ID, &meta.GroupID, &meta.Uploaded, &meta.Name, &meta.MIME, &meta.Size, &meta.SHA256)
	if err != nil {
		return "", errors.New("attachment is unavailable")
	}
	if meta.GroupID != groupID || groupID == "" {
		return "", ErrForbidden
	}
	var active int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_members WHERE group_id=? AND user_id=? AND status='active'`, groupID, sender).Scan(&active); err != nil || active == 0 {
		return "", ErrForbidden
	}
	canonical, _ := json.Marshal(MessageBody{Kind: "attachment", AttachmentID: meta.ID, Name: meta.Name, MIME: meta.MIME, Size: meta.Size})
	return string(canonical), nil
}

func ContentDisposition(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}
