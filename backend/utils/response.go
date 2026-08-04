package utils

import "github.com/gin-gonic/gin"

const (
	CodeSuccess         = 0
	CodeInvalidParams   = 1001
	CodeNotFound        = 1002
	CodeTooManyRequests = 1003
	CodeUnauthenticated = 2001
	CodeForbidden       = 2002
	CodeLoginFailed     = 2003
	CodeUsernameExists  = 2004
	CodeInternal        = 9999
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(200, Response{
		Code:    CodeSuccess,
		Message: "ok",
		Data:    data,
	})
}

func Error(c *gin.Context, code int, message string) {
	c.JSON(200, Response{
		Code:    code,
		Message: message,
		Data:    nil,
	})
}
