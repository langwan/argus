package main

import (
	"os"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Port                int    `yaml:"port"`
	Host                string `yaml:"host"`
	SenseVoicePort      int    `yaml:"sensevoice_port"`
	DataDir             string `yaml:"data_dir"`
	FFmpegPath          string `yaml:"ffmpeg_path"`
	FFprobePath         string `yaml:"ffprobe_path"`
	SubtitleCommonWords string `yaml:"subtitle_common_words"`
	ASR                 string `yaml:"asr"`
}

var config Config = Config{
	Port:           8090,
	SenseVoicePort: 8091,
	Host:           "localhost",
	DataDir:        "./data",
	ASR:            ASRProviderSenseVoice,
}

func loadConfig() error {
	data, err := os.ReadFile("config.yml")
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	return nil
}
