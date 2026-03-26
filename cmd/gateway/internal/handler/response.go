package handler

import (
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func writeError(r *http.Request, w http.ResponseWriter, code int, message string) {
	httpx.WriteJsonCtx(r.Context(), w, code, map[string]string{
		"error": message,
	})
}
