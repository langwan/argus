package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

	videoBaseDir := filepath.Join(config.DataDir, "store/videos")
	videoDir := filepath.Join(videoBaseDir, fmt.Sprintf("%d", ctx.VideoId))
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		return fmt.Errorf("failed to create video subdirectory: %v", err)
	}

	err := uploadVideoTaskStepUpload(ctx)
	if err != nil {
		return err
	}

	go func() {
		defer os.Remove(ctx.UploadFilePath)
		tasks := []VideoTaskFunc{
			uploadVideoTaskStepTranscode,
			uploadVideoTaskStepAudio,
			uploadVideoTaskStepSubtitle,
			uploadVideoTaskStepCover,
		}
		// Execute tasks one by one in a loop
		for _, task := range tasks {
			err := task(ctx)
			if err != nil {
				log.Printf("Task execution failed: %v\n", err)
				GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Updates(gin.H{"status": StatusFailed, "error": err.Error()})
				break
			}
		}
	}()

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

	out.Sync()
	out.Close()
	defaultTitle := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))

	ctx.UploadFilePath = tempPath

	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Updates(gin.H{"title": defaultTitle})

	return nil
}

func uploadVideoTaskStepTranscode(ctx *TaskContext) error {
	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Update("task_step", TaskStepTranscode)
	log.Printf("Transcoding video %d\n", ctx.VideoId)

	transcodedPath := getVideoFilePath(ctx, VideoFileTypeTranscoded)

	video := Video{}

	if err := GlobalDB.Where(&Video{ID: ctx.VideoId}).First(&video).Error; err != nil {
		return err
	}
	log.Printf("Transcoding uploadfile path %s, title: %s\n", ctx.UploadFilePath, video.Title)
	width, height, err := getVideoResolution(ctx.UploadFilePath)
	if err != nil {
		return fmt.Errorf("Failed to detect video resolution: %v", err)
	}

	rotation, err := getVideoRotation(ctx.UploadFilePath)
	if err != nil {
		log.Printf("Warning: Failed to detect video rotation information: %v\n", err)
		rotation = 0
	}

	var effectiveWidth, effectiveHeight int
	var aspectRatio float64

	if rotation == 90 || rotation == 270 {

		effectiveWidth = height
		effectiveHeight = width
	} else {

		effectiveWidth = width
		effectiveHeight = height
	}

	aspectRatio = float64(effectiveWidth) / float64(effectiveHeight)

	var targetWidth, targetHeight int

	if aspectRatio >= 1.0 {

		targetWidth = 1920
		targetHeight = int(1920 / aspectRatio)

		if targetHeight%2 != 0 {
			targetHeight += 1
		}

		if targetHeight > 1080 {
			targetHeight = 1080
			targetWidth = int(1080 * aspectRatio)
			if targetWidth%2 != 0 {
				targetWidth += 1
			}
		}
	} else {

		targetHeight = 1920
		targetWidth = int(1920 * aspectRatio)

		if targetWidth%2 != 0 {
			targetWidth += 1
		}

		if targetWidth > 1080 {
			targetWidth = 1080
			targetHeight = int(1080 / aspectRatio)
			if targetHeight%2 != 0 {
				targetHeight += 1
			}
		}
	}

	log.Printf("Detected video: original resolution %dx%d, rotation %d degrees, effective resolution %dx%d (aspect ratio %.2f, %s), target resolution: %dx%d\n",
		width, height, rotation, effectiveWidth, effectiveHeight, aspectRatio,
		map[bool]string{true: "landscape", false: "portrait"}[aspectRatio >= 1.0],
		targetWidth, targetHeight)

	var vfFilter string
	switch rotation {
	case 90:

		vfFilter = fmt.Sprintf("transpose=2,scale=%d:%d,setsar=1", targetWidth, targetHeight)
		log.Printf("Apply rotation filter: transpose=2 (counter-clockwise 90 degrees, correct 90 degrees rotation)\n", rotation)
	case 270:

		vfFilter = fmt.Sprintf("transpose=1,scale=%d:%d,setsar=1", targetWidth, targetHeight)
		log.Printf("Apply rotation filter: transpose=1 (clockwise 90 degrees, correct 270 degrees rotation)\n", rotation)
	case 180:

		vfFilter = fmt.Sprintf("transpose=2,transpose=2,scale=%d:%d,setsar=1", targetWidth, targetHeight)
		log.Printf("Apply rotation filter: transpose=2,transpose=2 (180 degrees)\n", rotation)
	default:

		vfFilter = fmt.Sprintf("scale=%d:%d,setsar=1", targetWidth, targetHeight)
		log.Printf("Apply filter: none + scale=%dx%d\n", targetWidth, targetHeight)
	}

	vfFilter = "scale='if(gt(iw,ih),1920,-2)':'if(gt(iw,ih),-2,1920)',setsar=1"

	args := []string{
		"-i", ctx.UploadFilePath,
		"-c:v", "libx264",
		"-crf", "23",
		"-preset", "medium",
		"-vf", vfFilter,
		"-c:a", "aac",
		"-b:a", "192k",
		"-y",
		transcodedPath,
	}

	// Create command
	cmd := exec.Command(config.FFmpegPath, args...)

	// Print full command for debugging
	fmt.Printf("Executing transcoding command: %s %v\n", config.FFmpegPath, args)

	// Get output (includes stdout and stderr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Video transcoding failed: %v, Details: %s", err, string(output))
	}

	fmt.Println("Video transcoding completed! Output path:", transcodedPath)
	return nil

}
func getVideoResolution(videoPath string) (int, int, error) {

	cmd := exec.Command(config.FFprobePath, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", videoPath)
	log.Printf("Executing ffprobe command: %s %v\n", config.FFprobePath, videoPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		cmd2 := exec.Command(config.FFmpegPath, "-i", videoPath)
		output2, _ := cmd2.CombinedOutput()
		return 1920, 1080, fmt.Errorf("ffprobe failed: %v, ffmpeg output: %s", err, string(output2))
	}

	resStr := strings.TrimSpace(string(output))
	parts := strings.Split(resStr, "x")

	var validParts []string
	for _, p := range parts {
		if p != "" {
			validParts = append(validParts, p)
		}
	}

	if len(validParts) < 2 {
		return 1920, 1080, fmt.Errorf("failed to parse resolution: %s", resStr)
	}

	width, err := strconv.Atoi(validParts[0])
	if err != nil {
		return 1920, 1080, err
	}

	height, err := strconv.Atoi(validParts[1])
	if err != nil {
		return 1920, 1080, err
	}

	return width, height, nil
}
func getVideoRotation(videoPath string) (int, error) {

	cmd := exec.Command(config.FFprobePath, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "side_data=rotation", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)

	output, err := cmd.CombinedOutput()
	if err == nil {
		rotationStr := strings.TrimSpace(string(output))
		if rotationStr != "" && rotationStr != "N/A" {
			if rotation, err := strconv.Atoi(rotationStr); err == nil {
				return rotation, nil
			}
			if rotation, err := strconv.ParseFloat(rotationStr, 64); err == nil {
				return int(rotation), nil
			}
		}
	}

	cmd2 := exec.Command(config.FFprobePath, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=side_data,tags", "-of", "json", videoPath)
	output2, err2 := cmd2.CombinedOutput()
	if err2 == nil {
		var result map[string]interface{}
		if err := json.Unmarshal(output2, &result); err == nil {
			if streams, ok := result["streams"].([]interface{}); ok && len(streams) > 0 {
				if stream, ok := streams[0].(map[string]interface{}); ok {

					if sideData, ok := stream["side_data"].([]interface{}); ok {
						for _, sd := range sideData {
							if data, ok := sd.(map[string]interface{}); ok {
								if rotation, ok := data["rotation"].(float64); ok {
									return int(rotation), nil
								}

								if dataType, ok := data["side_data_type"].(string); ok && dataType == "Display Matrix" {
									if displayMatrix, ok := data["displaymatrix"].(string); ok {

										if strings.Contains(displayMatrix, "rotation of 90.00 degrees") {
											return 90, nil
										} else if strings.Contains(displayMatrix, "rotation of 180.00 degrees") {
											return 180, nil
										} else if strings.Contains(displayMatrix, "rotation of 270.00 degrees") {
											return 270, nil
										}
									}
								}
							}
						}
					}

					if tags, ok := stream["tags"].(map[string]interface{}); ok {
						if rotation, ok := tags["rotate"].(string); ok {
							if rot, err := strconv.Atoi(rotation); err == nil {
								return rot, nil
							}
						}
						if rotation, ok := tags["rotate"].(float64); ok {
							return int(rotation), nil
						}
					}
				}
			}
		}
	}

	cmd3 := exec.Command(config.FFmpegPath, "-i", videoPath)
	output3, _ := cmd3.CombinedOutput()
	outputStr := string(output3)

	if strings.Contains(outputStr, "rotation of 90.00 degrees") {
		return 90, nil
	} else if strings.Contains(outputStr, "rotation of 180.00 degrees") {
		return 180, nil
	} else if strings.Contains(outputStr, "rotation of 270.00 degrees") {
		return 270, nil
	}

	width, height, err := getVideoResolution(videoPath)
	if err == nil {

		if height > width && (height == width*2 || height*9 == width*16) {

			return 90, nil
		}
	}

	return 0, nil
}

func uploadVideoTaskStepAudio(ctx *TaskContext) error {
	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Update("task_step", TaskStepAudio)
	transcodedPath := getVideoFilePath(ctx, VideoFileTypeTranscoded)
	audioPath := getVideoFilePath(ctx, VideoFileTypeAudio)

	cmd := exec.Command(config.FFmpegPath, "-i", transcodedPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", audioPath, "-y")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Audio conversion failed: %v", err)
		return fmt.Errorf("audio conversion failed: %v", err)
	}

	return nil
}

func uploadVideoTaskStepSubtitle(ctx *TaskContext) error {
	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Update("task_step", TaskStepSubtitle)

	audioPath := getVideoFilePath(ctx, VideoFileTypeAudio)

	asrProvider, err := GetASRProvider(config.ASR)
	if err != nil {
		return fmt.Errorf("Failed to get ASR provider: %v", err)
	}

	subtitlePath := getVideoFilePath(ctx, VideoFileTypeSubtitle)

	err = asrProvider.TranscribeToSRT(audioPath, "zh", config.SubtitleCommonWords, subtitlePath)
	if err != nil {
		return fmt.Errorf("[%s] Recognition failed: %v", asrProvider.Name(), err)
	}

	return nil
}

func uploadVideoTaskStepCover(ctx *TaskContext) error {
	GlobalDB.Model(&Video{}).Where("id = ?", ctx.VideoId).Update("task_step", TaskStepCover)
	transcodedPath := getVideoFilePath(ctx, VideoFileTypeTranscoded)
	coverPath := getVideoFilePath(ctx, VideoFileTypeCover)
	duration, err := getVideoDuration(transcodedPath)
	if err != nil {
		log.Printf("Warning: Failed to get video duration, using default 5th second: %v", err)
		duration = 10
	}

	// ========== Core Frame Capture Strategy ==========
	var captureTime float64
	const standardTime = 5.0

	if duration >= standardTime {
		// Video duration >= 5 seconds: Use fixed 5th second for consistency
		captureTime = standardTime
		log.Printf("Video duration: %.2fs >= 5 seconds, using standard capture time: %.2f seconds", duration, captureTime)
	} else {
		// Video duration < 5 seconds: Use middle frame, minimum 0.5 seconds (avoid black screen)
		captureTime = math.Max(0.5, duration/2)
		log.Printf("Video duration: %.2fs < 5 seconds, using middle point: %.2f seconds", duration, captureTime)
	}

	// Format time as HH:MM:SS format
	seconds := int(captureTime)
	frac := captureTime - float64(seconds)
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	timeStr := fmt.Sprintf("%02d:%02d:%02d.%.0f", hours, minutes, secs, frac*1000)

	// Use ffmpeg to capture frame at specified time as cover
	// -strict unofficial: Fix FFmpeg 8.x strict YUV range check issue with mjpeg
	cmd := exec.Command(config.FFmpegPath, "-i", transcodedPath, "-ss", timeStr, "-vframes", "1", "-q:v", "2", "-strict", "unofficial", coverPath, "-y")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// If failed, try using 1st second as fallback
		cmd2 := exec.Command(config.FFmpegPath, "-i", transcodedPath, "-ss", "00:00:01", "-vframes", "1", "-q:v", "2", "-strict", "unofficial", coverPath, "-y")
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("Cover capture failed: %v, output: %s", err2, string(output)+"\nRetry: "+string(output2))
		}
	}

	return nil
}

func getVideoPath(id int64) string {
	return filepath.Join(config.DataDir, "store/videos/"+strconv.FormatInt(id, 10))
}

type VideoFileType string

const (
	VideoFileTypeAudio      VideoFileType = "audio"
	VideoFileTypeTranscoded VideoFileType = "transcoded"
	VideoFileTypeSubtitle   VideoFileType = "subtitle"
	VideoFileTypeCover      VideoFileType = "cover"
)

func getVideoFilePath(ctx *TaskContext, videoType VideoFileType) string {
	videoBaseDir := filepath.Join(config.DataDir, fmt.Sprintf("store/videos/%d", ctx.VideoId))
	switch videoType {
	case VideoFileTypeTranscoded:
		return filepath.Join(videoBaseDir, "transcoded.mp4")
	case VideoFileTypeAudio:
		return filepath.Join(videoBaseDir, "audio.wav")
	case VideoFileTypeSubtitle:
		return filepath.Join(videoBaseDir, "subtitle.srt")
	case VideoFileTypeCover:
		return filepath.Join(videoBaseDir, "cover.jpg")
	}

	return videoBaseDir
}

type CoverSizeType string

const (
	CoverSizeType16x9 CoverSizeType = "16x9"
	CoverSizeType9x16 CoverSizeType = "9x16"
	CoverSizeType4x3  CoverSizeType = "4x3"
	CoverSizeType3x4  CoverSizeType = "3x4"
)

func getVideoFileCoverPath(ctx *TaskContext, sizeType CoverSizeType) string {
	videoBaseDir := filepath.Join(config.DataDir, fmt.Sprintf("store/videos/%d", ctx.VideoId))
	return filepath.Join(videoBaseDir, fmt.Sprintf("cover_%s.jpg", sizeType))
}
func getVideoDuration(videoPath string) (float64, error) {
	cmd := exec.Command(config.FFmpegPath, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=duration", "-of", "csv=p=0", videoPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("Failed to get video duration: %v, output: %s", err, string(output))
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("Failed to parse duration: %v", err)
	}

	return duration, nil
}
