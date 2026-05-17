package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var uploadTasks sync.Map

type TaskContext struct {
	Ctx            context.Context
	Cancel         context.CancelFunc
	UploadFilePath string
	VideoId        int64
	HttpContext    *gin.Context
}

type VideoTaskFunc func(ctx *TaskContext) error

func uploadVideoHandler(c *gin.Context) {

	video := Video{
		Status: StatusPending,
	}
	GlobalDB.Create(&video)
	ctx, cancel := context.WithCancel(context.Background())
	taskCtx := &TaskContext{
		Ctx:            ctx,
		Cancel:         cancel,
		UploadFilePath: "",
		VideoId:        video.ID,
		HttpContext:    c,
	}

	err := newUploadVideoTask(taskCtx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "video uploaded successfully",
	})
}

func newUploadVideoTask(ctx *TaskContext) error {
	if _, loaded := uploadTasks.LoadOrStore(ctx.VideoId, ctx); loaded {
		return nil
	}

	err := uploadVideoTaskStepUpload(ctx)
	if err != nil {
		return err
	}
	return nil
}

func uploadVideoTaskStepUpload(ctx *TaskContext) error {
	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Update("task_step", TaskStepUpload)
	tempDir := filepath.Join(config.DataDir, "temp")
	file, header, err := ctx.HttpContext.Request.FormFile("file")
	if err != nil {

		return err
	}
	defer file.Close()
	tempExt := filepath.Ext(header.Filename)
	tempFileName := fmt.Sprintf("upload_%d%s", time.Now().Unix(), tempExt)
	tempPath := filepath.Join(tempDir, tempFileName)

	out, err := os.Create(tempPath)
	if err != nil {

		return err
	}
	_, err = io.Copy(out, file)
	if err != nil {
		out.Close()
		os.Remove(tempPath)

		return err
	}

	// 确保文件句柄关闭（避免重命名时文件被占用）
	out.Sync()
	out.Close()
	defaultTitle := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))

	ctx.UploadFilePath = tempPath

	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Updates(gin.H{"title": defaultTitle})

	return nil
}
