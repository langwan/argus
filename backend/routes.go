package main

import "github.com/gin-gonic/gin"

func registerApi(api gin.IRouter) {
	api.POST("/videos/upload", uploadVideoHandler)
}
