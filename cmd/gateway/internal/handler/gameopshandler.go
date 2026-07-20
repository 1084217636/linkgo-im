package handler

import (
	"errors"
	"net"
	"net/http"
	"time"

	gwmiddleware "github.com/1084217636/linkgo-im/cmd/gateway/internal/middleware"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/1084217636/linkgo-im/internal/gameops"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ActivityDraftHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var req types.ActivityDraftReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		actor := gameOpsActor(r)
		version, err := svcCtx.ActivityOps.CreateDraft(r.Context(), actor, req.ActivityID, gameops.ActivityConfig{
			Title: req.Config.Title, StartAt: req.Config.StartAt, EndAt: req.Config.EndAt,
			RewardItemID: req.Config.RewardItemID, RewardQuantity: req.Config.RewardQuantity,
		}, req.RolloutPercent, requestID(r), r.Header.Get("X-Trace-ID"), requestClientIP(r))
		writeMeasuredGameOpsResponse(r, w, "activity.create_draft", started, version, err)
	}
}

func ActivitySubmitHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return activityTransitionHandler(svcCtx, "activity.submit", func(r *http.Request, actor gameops.Actor, req types.ActivityTransitionReq) (any, error) {
		err := svcCtx.ActivityOps.Submit(r.Context(), actor, req.ActivityID, req.Version, requestID(r), r.Header.Get("X-Trace-ID"), requestClientIP(r))
		return map[string]any{"activity_id": req.ActivityID, "version": req.Version, "status": gameops.ActivityPending}, err
	})
}

func ActivityPublishHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return activityTransitionHandler(svcCtx, "activity.publish", func(r *http.Request, actor gameops.Actor, req types.ActivityTransitionReq) (any, error) {
		return svcCtx.ActivityOps.Publish(r.Context(), actor, req.ActivityID, req.Version, requestID(r), r.Header.Get("X-Trace-ID"), requestClientIP(r))
	})
}

func ActivityRollbackHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var req types.ActivityRollbackReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		err := svcCtx.ActivityOps.Rollback(r.Context(), gameOpsActor(r), req.ActivityID, requestID(r), r.Header.Get("X-Trace-ID"), requestClientIP(r))
		writeMeasuredGameOpsResponse(r, w, "activity.rollback", started, map[string]any{"activity_id": req.ActivityID, "status": gameops.ActivityRolledBack}, err)
	}
}

func ItemGrantHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var req types.ItemGrantReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		items := make([]gameops.GrantItem, 0, len(req.Items))
		for _, item := range req.Items {
			items = append(items, gameops.GrantItem{UserID: item.UserID, ItemID: item.ItemID, Quantity: item.Quantity})
		}
		result, err := svcCtx.GrantOps.GrantItems(r.Context(), gameOpsActor(r), gameops.GrantRequest{GrantRequestID: req.GrantRequestID, Items: items}, r.Header.Get("X-Trace-ID"), requestClientIP(r))
		writeMeasuredGameOpsResponse(r, w, "item.batch_grant", started, result, err)
	}
}

func ItemGrantResultHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var req types.ItemGrantQueryReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		result, err := svcCtx.GrantOps.GetResult(r.Context(), req.GrantRequestID)
		writeMeasuredGameOpsResponse(r, w, "item.grant_result", started, result, err)
	}
}

func AuditListHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var req types.AuditQueryReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		entries, err := gameops.ListAudits(r.Context(), svcCtx.DB, gameops.AuditFilter{OperatorID: req.OperatorID, ResourceType: req.ResourceType, ResourceID: req.ResourceID, Result: req.Result, Limit: req.Limit})
		writeMeasuredGameOpsResponse(r, w, "audit.list", started, entries, err)
	}
}

func writeMeasuredGameOpsResponse(r *http.Request, w http.ResponseWriter, operation string, started time.Time, resp any, err error) {
	result := "success"
	if err != nil {
		result = "failed"
	}
	if errors.Is(err, gameops.ErrCacheSyncPending) {
		result = "cache_sync_pending"
	}
	metrics.GameOpsOperations.WithLabelValues(operation, result).Inc()
	metrics.GameOpsOperationLatencySeconds.WithLabelValues(operation).Observe(time.Since(started).Seconds())
	writeGameOpsResponse(r, w, resp, err)
}

func activityTransitionHandler(svcCtx *svc.ServiceContext, operation string, run func(*http.Request, gameops.Actor, types.ActivityTransitionReq) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var req types.ActivityTransitionReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		resp, err := run(r, gameOpsActor(r), req)
		writeMeasuredGameOpsResponse(r, w, operation, started, resp, err)
	}
}

func gameOpsActor(r *http.Request) gameops.Actor {
	return gameops.Actor{UserID: gwmiddleware.UserIDFromContext(r.Context()), Role: gwmiddleware.RoleFromContext(r.Context())}
}

func requestID(r *http.Request) string {
	if value := r.Header.Get("X-Request-ID"); value != "" {
		return value
	}
	return r.Header.Get("Idempotency-Key")
}

func requestClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeGameOpsResponse(r *http.Request, w http.ResponseWriter, resp any, err error) {
	if err == nil {
		httpx.WriteJsonCtx(r.Context(), w, http.StatusOK, resp)
		return
	}
	if errors.Is(err, gameops.ErrCacheSyncPending) {
		httpx.WriteJsonCtx(r.Context(), w, http.StatusAccepted, map[string]any{"data": resp, "warning": err.Error()})
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, gameops.ErrInvalidActivity), errors.Is(err, gameops.ErrInvalidGrant):
		status = http.StatusBadRequest
	case errors.Is(err, gameops.ErrInvalidState), errors.Is(err, gameops.ErrSelfApproval):
		status = http.StatusConflict
	case errors.Is(err, gameops.ErrForbidden):
		status = http.StatusForbidden
	}
	httpx.WriteJsonCtx(r.Context(), w, status, map[string]string{"error": err.Error()})
}
