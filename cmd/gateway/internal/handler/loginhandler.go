package handler

import (
	"net/http"
	"strings"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/logic"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func LoginHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.LoginReq
		if err := httpx.Parse(r, &req); err != nil {
			writeError(r, w, http.StatusBadRequest, err.Error())
			return
		}

		l := logic.NewLoginLogic(r.Context(), svcCtx)
		resp, err := l.Login(&req)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "invalid password") || strings.Contains(err.Error(), "user not found") {
				status = http.StatusUnauthorized
			}
			writeError(r, w, status, err.Error())
			return
		}

		httpx.OkJsonCtx(r.Context(), w, resp)
	}
}
