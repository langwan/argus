package main

import "time"

type TaskStep string

const (
	TaskStepUpload    TaskStep = "upload"
	TaskStepTranscode TaskStep = "transcode"
	TaskStepAudio     TaskStep = "audio"
	TaskStepSubtitle  TaskStep = "subtitle"
	TaskStepCover     TaskStep = "cover"
)

type VideoStatus string

const (
	StatusPending    VideoStatus = "pending"
	StatusProcessing VideoStatus = "processing"
	StatusCompleted  VideoStatus = "completed"
	StatusFailed     VideoStatus = "failed"
)

const (
	ASRProviderWhisper    = "whisper"
	ASRProviderSenseVoice = "sensevoice"
)

type Preferences struct {
	ID        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	PrefKey   string `gorm:"type:varchar(255);uniqueIndex;not null;column:pref_key" json:"prefKey"`
	PrefValue string `gorm:"type:text;column:pref_value" json:"prefValue"`
}

type Video struct {
	ID        int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	Title     string      `gorm:"type:varchar(255)" json:"title"`
	TaskStep  TaskStep    `gorm:"type:varchar(32)" json:"task_step"`
	Status    VideoStatus `gorm:"type:varchar(32);default:'pending'" json:"status"`
	Error     string      `gorm:"type:text" json:"error"`
	CreatedAt time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}

func (v *Video) TableName() string {
	return "videos"
}

func tables() []any {
	return []any{
		&Preferences{},
		&Video{},
	}
}
