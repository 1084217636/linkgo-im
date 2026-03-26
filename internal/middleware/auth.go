package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = []byte("linkgo_im_secret_2026")

type MyClaims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func SetJWTSecret(secret string) {
	if secret == "" {
		return
	}

	jwtSecret = []byte(secret)
}

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

func ParseToken(tokenString string) (*MyClaims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, errors.New("missing token")
	}

	token, err := jwt.ParseWithClaims(tokenString, &MyClaims{}, func(t *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(*MyClaims)
	if !ok || claims.UserID == "" {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

func ExtractBearerToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return value
}
