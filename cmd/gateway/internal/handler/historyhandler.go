package handler

import (
	"net/http"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/logic"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func HistoryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.HistoryReq
		if err := httpx.Parse(r, &req); err != nil {
			writeError(r, w, http.StatusBadRequest, err.Error())
			return
		}

		l := logic.NewHistoryLogic(r.Context(), svcCtx)
		resp, err := l.GetHistory(&req)
		if err != nil {
			status := http.StatusInternalServerError
			if err.Error() == "target_id is required" {
				status = http.StatusBadRequest
			}
			writeError(r, w, status, err.Error())
			return
		}

		httpx.OkJsonCtx(r.Context(), w, resp)
	}
}
