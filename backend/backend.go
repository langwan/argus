package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
)

func main() {
	// Original initialization logic
	loadConfig()
	initDataDir()
	initDB()

	// Start Python service asynchronously before starting Gin
	StartPythonServiceAsync()

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

	// Core modification: Use native http.Server for lifecycle control
	srv := &http.Server{
		Addr:    config.Host + ":" + strconv.Itoa(config.Port),
		Handler: r,
	}

	// Start Gin server in a separate goroutine to avoid blocking main thread
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen: %s\n", err)
		}
	}()

	// Intercept system shutdown signals
	quit := make(chan os.Signal, 1)
	// SIGINT (Ctrl+C), SIGTERM (kill PID command)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Code blocks here until program is closed
	<-quit

	log.Println("[Shutdown] Shutdown signal received. Executing cleanup...")

	// Cleanup Step 1: Kill Python subprocess
	if globalPythonCmd != nil && globalPythonCmd.Process != nil {
		log.Println("[Shutdown] Sending KILL signal to Python process...")
		err := globalPythonCmd.Process.Kill()
		if err != nil {
			log.Printf("[Shutdown] Failed to kill Python process: %v\n", err)
		} else {
			log.Println("[Shutdown] Background Python process successfully cleaned up.")
		}
	}

	// Cleanup Step 2: Gracefully shutdown Go HTTP server
	// Give HTTP server 5 seconds to finish pending requests, force close after 5 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown:", err)
	}

	log.Println("[Shutdown] Argus backend exited cleanly.")
}

func initDataDir() {
	// Initialize data directory
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		panic(err)
	}
	// Initialize database directory
	if err := os.MkdirAll(filepath.Join(config.DataDir, "store/db"), 0755); err != nil {
		panic(err)
	}
	// Initialize temporary directory
	if err := os.MkdirAll(filepath.Join(config.DataDir, "temp"), 0755); err != nil {
		panic(err)
	}
	// Initialize video directory
	if err := os.MkdirAll(filepath.Join(config.DataDir, "store/videos"), 0755); err != nil {
		panic(err)
	}
	// Initialize cache directory
	if err := os.MkdirAll(filepath.Join(config.DataDir, "cache"), 0755); err != nil {
		panic(err)
	}
}
