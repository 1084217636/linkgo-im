package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/attachment"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func RegisterAttachmentRoutes(server *rest.Server, svcCtx *svc.ServiceContext) {
	dir := os.Getenv("LINKGO_UPLOAD_DIR")
	if dir == "" {
		dir = "./uploads"
	}
	objects, err := attachment.ObjectStoreFromEnv()
	if err != nil {
		logx.Must(err)
	}
	store := &attachment.Store{DB: svcCtx.DB, Dir: dir, Objects: objects}
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{
		gwmiddleware.NewAuthMiddleware().Handle,
		gwmiddleware.NewRateLimitMiddleware(svcCtx.RestLimiter).Handle,
	},
		rest.Route{Method: http.MethodPost, Path: "/groups/:group_id/attachments", Handler: uploadAttachment(store)},
		rest.Route{Method: http.MethodGet, Path: "/attachments/:attachment_id/download", Handler: downloadAttachment(store)},
	), rest.WithPrefix("/api/v1"), rest.WithMaxBytes(attachment.MaxUploadBytes+(1<<20)))
}

func uploadAttachment(store *attachment.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := pathvar.Vars(r)["group_id"]
		if groupID == "" {
			http.Error(w, "group_id is required", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxUploadBytes+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			metrics.AttachmentOperations.WithLabelValues("upload", "invalid").Inc()
			http.Error(w, "invalid multipart upload or file too large", http.StatusBadRequest)
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			metrics.AttachmentOperations.WithLabelValues("upload", "invalid").Inc()
			http.Error(w, "file is required", http.StatusBadRequest)
			return
		}
		defer file.Close()
		meta, err := store.Upload(r.Context(), gwmiddleware.UserIDFromContext(r.Context()), groupID, filepath.Base(header.Filename), io.LimitReader(file, attachment.MaxUploadBytes+1))
		if err != nil {
			status := http.StatusBadRequest
			result := "invalid"
			if errors.Is(err, attachment.ErrForbidden) {
				status = http.StatusForbidden
				result = "forbidden"
			}
			metrics.AttachmentOperations.WithLabelValues("upload", result).Inc()
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		metrics.AttachmentOperations.WithLabelValues("upload", "success").Inc()
		metrics.AttachmentUploadBytes.Observe(float64(meta.Size))
		_ = json.NewEncoder(w).Encode(meta)
	}
}

func downloadAttachment(store *attachment.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathvar.Vars(r)["attachment_id"]
		meta, file, err := store.AuthorizedFile(r.Context(), gwmiddleware.UserIDFromContext(r.Context()), id)
		if err != nil {
			metrics.AttachmentOperations.WithLabelValues("download", "denied_or_missing").Inc()
			status := http.StatusForbidden
			if !errors.Is(err, attachment.ErrForbidden) {
				status = http.StatusNotFound
			}
			http.Error(w, "attachment unavailable", status)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Type", meta.MIME)
		w.Header().Set("Content-Length", fmtInt(meta.Size))
		w.Header().Set("Content-Disposition", attachment.ContentDisposition(meta.Name))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, file); err != nil {
			metrics.AttachmentOperations.WithLabelValues("download", "stream_error").Inc()
			return
		}
		metrics.AttachmentOperations.WithLabelValues("download", "success").Inc()
	}
}

func fmtInt(v int64) string { return strconv.FormatInt(v, 10) }
