package services

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"fan-web/models"
)

func newTestAuth() *AuthService {
	return NewAuthService("test-secret-key-for-auth-service", 24*time.Hour)
}

func TestIssueAndParseToken(t *testing.T) {
	auth := newTestAuth()
	token, expiresAt, err := auth.IssueToken(models.User{ID: 1, Username: "alice", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt.Before(time.Now()) {
		t.Fatal("expiresAt should be in the future")
	}
	claims, err := auth.ParseToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 1 || claims.Username != "alice" || !claims.IsAdmin {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestMediaTokenOnlyParsesThroughMediaParser(t *testing.T) {
	auth := newTestAuth()

	loginToken, _, err := auth.IssueToken(models.User{ID: 1, Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	// 登录 JWT 不能作为媒体票据。
	if _, err := auth.ParseMediaToken(loginToken, 5); err == nil {
		t.Fatal("login JWT must not pass media parser")
	}
	// 登录 JWT 应通过普通解析器。
	if _, err := auth.ParseToken(loginToken); err != nil {
		t.Fatal(err)
	}

	mediaToken, _, err := auth.IssueMediaToken(1, 5)
	if err != nil {
		t.Fatal(err)
	}
	// 媒体票据不能作为登录 JWT。
	if _, err := auth.ParseToken(mediaToken); err == nil {
		t.Fatal("media token must not pass login parser")
	}
	// 媒体票据应通过媒体解析器。
	if _, err := auth.ParseMediaToken(mediaToken, 5); err != nil {
		t.Fatal(err)
	}
}

func TestMediaTokenRejectsWrongEpisode(t *testing.T) {
	auth := newTestAuth()
	mediaToken, _, err := auth.IssueMediaToken(1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseMediaToken(mediaToken, 6); err == nil {
		t.Fatal("media token for episode 5 must not be valid for episode 6")
	}
}

func TestMediaTokenExpires(t *testing.T) {
	auth := newTestAuth()
	// 手工构造已过期的媒体票据，验证过期校验。
	now := time.Now()
	claims := MediaClaims{
		UserID:    1,
		EpisodeID: 5,
		TokenUse:  mediaTokenUse,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{mediaAudience},
			IssuedAt:  jwt.NewNumericDate(now.Add(-24 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret, _ := auth.snapshot()
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseMediaToken(signed, 5); err == nil {
		t.Fatal("expired media token must be rejected")
	}
}

func TestMediaTokenRequiresExpiration(t *testing.T) {
	auth := newTestAuth()
	now := time.Now()
	claims := MediaClaims{
		UserID:    1,
		EpisodeID: 5,
		TokenUse:  mediaTokenUse,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   tokenIssuer,
			Audience: jwt.ClaimStrings{mediaAudience},
			IssuedAt: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret, _ := auth.snapshot()
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseMediaToken(signed, 5); err == nil {
		t.Fatal("media token without exp must be rejected")
	}
}

func TestMediaTokenRejectsInvalidClaims(t *testing.T) {
	auth := newTestAuth()
	now := time.Now()
	base := MediaClaims{
		UserID:    1,
		EpisodeID: 5,
		TokenUse:  mediaTokenUse,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{mediaAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}

	tests := []struct {
		name   string
		mutate func(*MediaClaims)
	}{
		{name: "wrong issuer", mutate: func(c *MediaClaims) { c.Issuer = "other" }},
		{name: "wrong audience", mutate: func(c *MediaClaims) { c.Audience = jwt.ClaimStrings{"other"} }},
		{name: "wrong token use", mutate: func(c *MediaClaims) { c.TokenUse = "login" }},
		{name: "non-positive user", mutate: func(c *MediaClaims) { c.UserID = 0 }},
		{name: "non-positive episode", mutate: func(c *MediaClaims) { c.EpisodeID = 0 }},
	}

	secret, _ := auth.snapshot()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims := base
			tc.mutate(&claims)
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			signed, err := token.SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := auth.ParseMediaToken(signed, 5); err == nil {
				t.Fatal("invalid media claims must be rejected")
			}
		})
	}

	wronglySigned := jwt.NewWithClaims(jwt.SigningMethodHS256, base)
	signed, err := wronglySigned.SignedString([]byte("different-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseMediaToken(signed, 5); err == nil {
		t.Fatal("media token with wrong signature must be rejected")
	}
}

func TestUpdateConfigChangesSigningKey(t *testing.T) {
	auth := newTestAuth()
	token, _, err := auth.IssueToken(models.User{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 轮换密钥后旧 token 失效。
	auth.UpdateConfig("rotated-secret-key-456", 24*time.Hour)
	if _, err := auth.ParseToken(token); err == nil {
		t.Fatal("old token must be rejected after secret rotation")
	}
	// 新密钥可签发和解析。
	newToken, _, err := auth.IssueToken(models.User{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseToken(newToken); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentTokenIssuance(t *testing.T) {
	auth := newTestAuth()
	done := make(chan struct{})
	for i := 0; i < 32; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			token, _, err := auth.IssueToken(models.User{ID: int64(n)})
			if err != nil {
				t.Errorf("issue token %d: %v", n, err)
				return
			}
			if _, err := auth.ParseToken(token); err != nil {
				t.Errorf("parse token %d: %v", n, err)
			}
		}(i)
	}
	for i := 0; i < 32; i++ {
		<-done
	}
}
