package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func uploadVideoHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "video uploaded successfully",
	})
}
