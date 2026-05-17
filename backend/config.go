package main

import (
	"os"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Port                int
	Host                string
	DataDir             string
	FFmpegPath          string
	FFprobePath         string
	SubtitleCommonWords string
}

var config Config = Config{
	Port:    8090,
	Host:    "localhost",
	DataDir: "./data",
}

func loadConfig() error {
	data, err := os.ReadFile("config.yml")
	if err != nil {
		return err
	}
	var cfg struct {
		FfmpegPath  string `yaml:"ffmpeg_path"`
		FFprobePath string `yaml:"ffprobe_path"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	config.FFmpegPath = cfg.FfmpegPath
	config.FFprobePath = cfg.FFprobePath
	return nil
}
