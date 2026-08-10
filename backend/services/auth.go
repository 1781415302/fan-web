package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"fan-web/models"
)

const (
	tokenIssuer   = "fan-web"
	mediaAudience = "fan-web-media"
	mediaTokenUse = "media"
	mediaTokenTTL = 12 * time.Hour
)

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	TokenUse string `json:"token_use,omitempty"`
	jwt.RegisteredClaims
}

// MediaClaims 是媒体票据的签名载荷，用途限定为单个 episode 的视频/字幕。
type MediaClaims struct {
	UserID    int64  `json:"user_id"`
	EpisodeID int64  `json:"episode_id"`
	TokenUse  string `json:"token_use"`
	jwt.RegisteredClaims
}

type AuthService struct {
	mu     sync.RWMutex
	secret []byte
	expire time.Duration
}

func NewAuthService(secret string, expire time.Duration) *AuthService {
	return &AuthService{
		secret: cloneBytes([]byte(secret)),
		expire: expire,
	}
}

// UpdateConfig 原子更新密钥与过期时间。传入字符串对应的字节会被复制，
// 不保存调用方可变切片。
func (s *AuthService) UpdateConfig(secret string, expire time.Duration) {
	if len(secret) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secret = cloneBytes([]byte(secret))
	s.expire = expire
}

func cloneBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	out := make([]byte, len(src))
	copy(out, src)
	return out
}

func (s *AuthService) snapshot() ([]byte, time.Duration) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBytes(s.secret), s.expire
}

func (s *AuthService) IssueToken(user models.User) (string, time.Time, error) {
	secret, expire := s.snapshot()
	if len(secret) == 0 {
		return "", time.Time{}, fmt.Errorf("jwt secret 不能为空")
	}

	now := time.Now()
	expiresAt := now.Add(expire)
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
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	secret, _ := s.snapshot()
	if len(secret) == 0 {
		return nil, fmt.Errorf("jwt secret 不能为空")
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("不支持的 jwt 签名算法")
		}
		return secret, nil
	}, jwt.WithIssuer(tokenIssuer))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("jwt 无效")
	}
	if claims.TokenUse == mediaTokenUse {
		return nil, fmt.Errorf("媒体票据不能作为登录凭证")
	}
	return claims, nil
}

// IssueMediaToken 签发有效期 12 小时的媒体票据，仅可访问指定 episode。
func (s *AuthService) IssueMediaToken(userID, episodeID int64) (string, time.Time, error) {
	secret, _ := s.snapshot()
	if len(secret) == 0 {
		return "", time.Time{}, fmt.Errorf("jwt secret 不能为空")
	}
	if userID <= 0 || episodeID <= 0 {
		return "", time.Time{}, fmt.Errorf("无效的用户或集数 ID")
	}

	now := time.Now()
	expiresAt := now.Add(mediaTokenTTL)
	claims := MediaClaims{
		UserID:    userID,
		EpisodeID: episodeID,
		TokenUse:  mediaTokenUse,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{mediaAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// ParseMediaToken 解析并校验媒体票据：签名、issuer、audience、token use、
// 过期时间、正数 user/episode ID 以及票据 episode 与期望 episode 一致。
func (s *AuthService) ParseMediaToken(tokenString string, expectedEpisodeID int64) (*MediaClaims, error) {
	secret, _ := s.snapshot()
	if len(secret) == 0 {
		return nil, fmt.Errorf("jwt secret 不能为空")
	}
	if expectedEpisodeID <= 0 {
		return nil, fmt.Errorf("无效的集数 ID")
	}

	claims := &MediaClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("不支持的 jwt 签名算法")
		}
		return secret, nil
	},
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(mediaAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("媒体票据无效")
	}
	if claims.TokenUse != mediaTokenUse {
		return nil, fmt.Errorf("不支持的票据用途")
	}
	if claims.UserID <= 0 || claims.EpisodeID <= 0 {
		return nil, fmt.Errorf("票据内容无效")
	}
	if claims.EpisodeID != expectedEpisodeID {
		return nil, fmt.Errorf("票据与集数不匹配")
	}
	return claims, nil
}
