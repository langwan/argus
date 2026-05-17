package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
)

func main() {
	initDataDir()
	initDB()

	color.Green("argus backend running")
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "online",
			"message": "argus backend running",
		})
	})

	api := r.Group("/api")
	registerApi(api)

	r.Run(config.Host + ":" + strconv.Itoa(config.Port))
}

func initDataDir() {
	// 初始化数据目录
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		panic(err)
	}
	// 初始化数据库目录
	if err := os.MkdirAll(filepath.Join(config.DataDir, "store/db"), 0755); err != nil {
		panic(err)
	}
	// 初始化临时目录
	if err := os.MkdirAll(filepath.Join(config.DataDir, "temp"), 0755); err != nil {
		panic(err)
	}
	// 初始化视频目录
	if err := os.MkdirAll(filepath.Join(config.DataDir, "store/videos"), 0755); err != nil {
		panic(err)
	}
	// 初始化视频目录
	if err := os.MkdirAll(filepath.Join(config.DataDir, "cache"), 0755); err != nil {
		panic(err)
	}
}
