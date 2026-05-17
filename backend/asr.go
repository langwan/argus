package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ASRResult struct {
	Text     string
	Language string
	Duration float64
}

type ASRProvider interface {
	Name() string

	TranscribeToSRT(audioFile string, lang string, hotwords string, outputSRTPath string) error
}

type SenseVoiceProvider struct{}

func NewSenseVoiceProvider() *SenseVoiceProvider {
	return &SenseVoiceProvider{}
}

func (p *SenseVoiceProvider) Name() string {
	return ASRProviderSenseVoice
}

func (p *SenseVoiceProvider) TranscribeToSRT(audioFile string, lang string, hotwords string, outputSRTPath string) error {
	// Read default ASR engine configuration from database

	sensevoiceAddr := fmt.Sprintf("http://localhost:%d", config.SenseVoicePort)

	audioData, err := os.ReadFile(audioFile)
	if err != nil {
		return fmt.Errorf("failed to read audio file: %v", err)
	}

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	part, err := writer.CreateFormFile("file", filepath.Base(audioFile))
	if err != nil {
		return fmt.Errorf("failed to create form: %v", err)
	}
	part.Write(audioData)

	// FunASR hotword format: "word1 15 word2 15 word3 15"
	if hotwords != "" {
		words := strings.Split(hotwords, ",")
		var funasrHotwords []string
		for _, w := range words {
			w = strings.TrimSpace(w)
			if w != "" {
				funasrHotwords = append(funasrHotwords, fmt.Sprintf("%s 15", w))
			}
		}
		hotwordsStr := strings.Join(funasrHotwords, " ")
		writer.WriteField("hotwords", hotwordsStr)
		fmt.Printf("[Paraformer] Hotwords enabled: %s\n", hotwordsStr)
	}

	writer.Close()

	apiURL := sensevoiceAddr
	if !strings.HasSuffix(apiURL, "/") {
		apiURL += "/"
	}
	apiURL += "api/subtitle"

	req, err := http.NewRequest("POST", apiURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 900 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("SenseVoice API request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SenseVoice API returned error: status=%d, body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		SRT      string  `json:"srt"`
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if err := os.WriteFile(outputSRTPath, []byte(result.SRT), 0644); err != nil {
		return fmt.Errorf("failed to write subtitle file: %v", err)
	}

	fmt.Printf("[SenseVoice Paraformer] Subtitle generation completed: duration=%.2fs, saved to: %s\n", result.Duration, outputSRTPath)
	return nil
}

func GetASRProvider(providerName string) (ASRProvider, error) {
	switch providerName {
	case ASRProviderSenseVoice:
		return NewSenseVoiceProvider(), nil
	default:
		return nil, fmt.Errorf("unknown ASR provider: %s", providerName)
	}
}
