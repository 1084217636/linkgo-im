package handler

import (
	"net/http"

	"github.com/1084217636/linkgo-im/cmd/gateway/internal/logic"
	"github.com/1084217636/linkgo-im/cmd/gateway/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func UserGroupsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logic.NewUserGroupsLogic(r.Context(), svcCtx)
		resp, err := l.List()
		if err != nil {
			writeError(r, w, http.StatusInternalServerError, err.Error())
			return
		}

		httpx.OkJsonCtx(r.Context(), w, resp)
	}
}
