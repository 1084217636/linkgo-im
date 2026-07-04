package handler

import (
	"net/http"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/logic"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func AISummaryHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AISummaryReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := logic.NewAISummaryLogic(r.Context(), svcCtx)
		resp, err := l.Generate(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		httpx.OkJsonCtx(r.Context(), w, resp)
	}
}
