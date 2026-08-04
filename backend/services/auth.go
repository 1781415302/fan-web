package services

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"fan-web/models"
)

const tokenIssuer = "fan-web"

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

type AuthService struct {
	secret []byte
	expire time.Duration
}

func NewAuthService(secret string, expire time.Duration) *AuthService {
	return &AuthService{
		secret: []byte(secret),
		expire: expire,
	}
}

func (s *AuthService) IssueToken(user models.User) (string, time.Time, error) {
	if len(s.secret) == 0 {
		return "", time.Time{}, fmt.Errorf("jwt secret 不能为空")
	}

	now := time.Now()
	expiresAt := now.Add(s.expire)
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	if len(s.secret) == 0 {
		return nil, fmt.Errorf("jwt secret 不能为空")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("不支持的 jwt 签名算法")
		}
		return s.secret, nil
	}, jwt.WithIssuer(tokenIssuer))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("jwt 无效")
	}
	return claims, nil
}
