package handlers

import (
	"github.com/gin-gonic/gin"

	"fan-web/utils"
)

func Health(c *gin.Context) {
	utils.Success(c, gin.H{
		"status": "ok",
	})
}
