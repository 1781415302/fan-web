package services

import (
	"strings"
	"testing"
)

func TestValidateNewCredentials(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{"valid", "user", "password123", false},
		{"empty username", "  ", "password123", true},
		{"username too long", strings.Repeat("名", 65), "password123", true},
		{"password too short", "user", "short", true},
		{"password exactly 72 bytes", "user", strings.Repeat("a", 72), false},
		{"password exceeds 72 bytes", "user", strings.Repeat("a", 73), true},
		{"password not trimmed", "user", " password ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewCredentials(tc.username, tc.password)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
