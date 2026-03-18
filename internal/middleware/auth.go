package middleware

import (
	"time"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

var jwtSecret = []byte("linkgo_im_secret_2026")

type MyClaims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// --- 新增：给 Login 逻辑使用的生成函数 ---
func GenerateToken(userID string) (string, error) {
	claims := MyClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// Auth 统一鉴权中间件
func Auth() gin.HandlerFunc {
    return func(c *gin.Context) {
        var tokenString string

        // 1. 尝试从 Header 获取 (适用于 RESTful 接口)
        authHeader := c.GetHeader("Authorization")
        if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
            tokenString = authHeader[7:]
        }

        // 2. 如果 Header 没有，尝试从 URL 参数获取 (适用于 WebSocket)
        if tokenString == "" {
            tokenString = c.Query("token")
        }

        // 3. 校验 Token 是否存在
        if tokenString == "" {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "未提供访问凭证"})
            c.Abort()
            return
        }

        // 4. 解析并校验 JWT
        token, err := jwt.ParseWithClaims(tokenString, &MyClaims{}, func(t *jwt.Token) (interface{}, error) {
            return jwtSecret, nil
        })

        if err != nil || !token.Valid {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "无效或过期的凭证"})
            c.Abort()
            return
        }

        // 5. 将解析出的 UserID 存入 Context，方便后续业务逻辑使用
        claims, ok := token.Claims.(*MyClaims)
        if ok {
            c.Set("user_id", claims.UserID)
        }

        c.Next()
    }
}
