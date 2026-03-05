package middleware

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

// Auth 鉴权中间件
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		// 简历提到 JWT 鉴权
		// 这里简化为非空校验，实际可在此解析校验 JWT
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权的连接"})
			c.Abort()
			return
		}
		c.Next()
	}
}
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token == "mock_token_123" { // 模拟鉴权
            c.Next()
        } else {
            c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
        }
    }
}