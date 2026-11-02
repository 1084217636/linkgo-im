package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/internal/learningtask"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

// RegisterLearningTaskRoutes keeps the task API together while the existing
// generated gateway routes continue to serve the original IM contract.
func RegisterLearningTaskRoutes(server *rest.Server, svcCtx *svc.ServiceContext) {
	api := &taskAPI{service: learningtask.NewService(svcCtx.DB)}
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{
		gwmiddleware.NewAuthMiddleware().Handle,
		gwmiddleware.NewRateLimitMiddleware(svcCtx.RestLimiter).Handle,
	},
		rest.Route{Method: http.MethodPost, Path: "/groups/:group_id/tasks", Handler: observeTask("create", api.create)},
		rest.Route{Method: http.MethodGet, Path: "/groups/:group_id/tasks", Handler: observeTask("list", api.list)},
		rest.Route{Method: http.MethodGet, Path: "/tasks/:task_id", Handler: observeTask("detail", api.detail)},
		rest.Route{Method: http.MethodPatch, Path: "/tasks/:task_id", Handler: observeTask("edit", api.edit)},
		rest.Route{Method: http.MethodPost, Path: "/tasks/:task_id/publish", Handler: observeTask("publish", api.publish)},
		rest.Route{Method: http.MethodPut, Path: "/tasks/:task_id/submission", Handler: observeTask("submit", api.submit)},
		rest.Route{Method: http.MethodGet, Path: "/tasks/:task_id/submissions", Handler: observeTask("submissions", api.submissions)},
		rest.Route{Method: http.MethodGet, Path: "/tasks/:task_id/progress", Handler: observeTask("progress", api.progress)},
		rest.Route{Method: http.MethodPost, Path: "/tasks/:task_id/close", Handler: observeTask("close", api.close)},
		rest.Route{Method: http.MethodPost, Path: "/submissions/:submission_id/review", Handler: observeTask("review", api.review)},
	), rest.WithPrefix("/api/v1"))
}

type taskAPI struct{ service *learningtask.Service }

type taskStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *taskStatusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *taskStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func observeTask(operation string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &taskStatusWriter{ResponseWriter: w}
		next(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.LearningTaskRequests.WithLabelValues(operation, strconv.Itoa(status/100)+"xx").Inc()
		metrics.LearningTaskLatencySeconds.WithLabelValues(operation).Observe(time.Since(started).Seconds())
	}
}

type taskPath struct {
	TaskID       string `path:"task_id"`
	GroupID      string `path:"group_id"`
	SubmissionID string `path:"submission_id"`
}
type taskBody struct {
	RequestID       string   `json:"request_id"`
	ExpectedVersion int64    `json:"expected_version"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	DueAt           int64    `json:"due_at"`
	Text            string   `json:"text"`
	Link            string   `json:"link"`
	Decision        string   `json:"decision"`
	Reason          string   `json:"reason"`
	AttachmentIDs   []string `json:"attachment_ids"`
}

func parseTaskPath(r *http.Request) (taskPath, error) {
	vars := pathvar.Vars(r)
	p := taskPath{TaskID: vars["task_id"], GroupID: vars["group_id"], SubmissionID: vars["submission_id"]}
	if p.TaskID == "" && p.GroupID == "" && p.SubmissionID == "" {
		return p, errors.New("resource id is required")
	}
	return p, nil
}
func parseTaskBody(r *http.Request) (taskBody, error) {
	var b taskBody
	// A 5000-rune UTF-8 description can require 20 KiB before JSON overhead.
	r.Body = http.MaxBytesReader(nil, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return b, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return b, errors.New("one JSON object is required")
	}
	return b, nil
}
func taskActor(r *http.Request) string { return gwmiddleware.UserIDFromContext(r.Context()) }
func taskWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func taskError(w http.ResponseWriter, err error) {
	status := learningtask.StatusCode(err)
	message := err.Error()
	if status == http.StatusServiceUnavailable {
		message = "task service unavailable"
	}
	response := map[string]any{"error": message}
	var domainErr *learningtask.Error
	if errors.As(err, &domainErr) && domainErr.CurrentVersion != nil {
		response["current_version"] = *domainErr.CurrentVersion
	}
	taskWrite(w, status, response)
}
func taskInputError(w http.ResponseWriter, err error) {
	taskWrite(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}
func taskLimit(r *http.Request) int { v, _ := strconv.Atoi(r.URL.Query().Get("limit")); return v }

func (a *taskAPI) create(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	b, e := parseTaskBody(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Create(r.Context(), taskActor(r), p.GroupID, b.RequestID, b.Title, b.Description, b.DueAt)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, http.StatusCreated, v)
}
func (a *taskAPI) edit(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	b, e := parseTaskBody(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.UpdateDraft(r.Context(), taskActor(r), p.TaskID, b.RequestID, b.ExpectedVersion, b.Title, b.Description, b.DueAt)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) publish(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	b, e := parseTaskBody(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Publish(r.Context(), taskActor(r), p.TaskID, b.RequestID, b.ExpectedVersion)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) close(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	b, e := parseTaskBody(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Close(r.Context(), taskActor(r), p.TaskID, b.RequestID, b.ExpectedVersion)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) submit(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	b, e := parseTaskBody(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Submit(r.Context(), taskActor(r), p.TaskID, b.RequestID, b.Text, b.Link, b.ExpectedVersion, b.AttachmentIDs)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) review(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	b, e := parseTaskBody(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Review(r.Context(), taskActor(r), p.SubmissionID, b.RequestID, b.Decision, b.Reason, b.ExpectedVersion)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) list(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.List(r.Context(), taskActor(r), p.GroupID, r.URL.Query().Get("cursor"), taskLimit(r))
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) detail(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Get(r.Context(), taskActor(r), p.TaskID)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) submissions(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.ListSubmissions(r.Context(), taskActor(r), p.TaskID, r.URL.Query().Get("cursor"), taskLimit(r))
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
func (a *taskAPI) progress(w http.ResponseWriter, r *http.Request) {
	p, e := parseTaskPath(r)
	if e != nil {
		taskInputError(w, e)
		return
	}
	v, e := a.service.Progress(r.Context(), taskActor(r), p.TaskID)
	if e != nil {
		taskError(w, e)
		return
	}
	taskWrite(w, 200, v)
}
