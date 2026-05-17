package main

import (
	"net/http"
	"strconv"

	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
)

func main() {

	color.Green("argus backend running")
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "online",
			"message": "argus backend running",
		})
	})

	r.Run(config.Host + ":" + strconv.Itoa(config.Port))
}
