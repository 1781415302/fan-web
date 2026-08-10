package services

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxUsernameChars = 64
	minPasswordChars = 8
	maxPasswordBytes = 72
)

// ValidateNewCredentials 校验新建账号的用户名与密码规则。
// 密码不做 trim，避免静默改变用户输入。
func ValidateNewCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if utf8.RuneCountInString(username) > maxUsernameChars {
		return fmt.Errorf("用户名最多 %d 个字符", maxUsernameChars)
	}
	if utf8.RuneCountInString(password) < minPasswordChars {
		return fmt.Errorf("密码至少 %d 个字符", minPasswordChars)
	}
	if len([]byte(password)) > maxPasswordBytes {
		return fmt.Errorf("密码长度不能超过 %d 字节", maxPasswordBytes)
	}
	return nil
}
