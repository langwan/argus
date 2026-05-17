package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)


var globalPythonCmd *exec.Cmd

func getVenvPaths() (string, string) {
	wd, _ := os.Getwd()
	var pythonPath, pipPath string

	if runtime.GOOS == "windows" {
		pythonPath = filepath.Join(wd, "venv", "Scripts", "python.exe")
		pipPath = filepath.Join(wd, "venv", "Scripts", "pip.exe")
	} else {
		pythonPath = filepath.Join(wd, "venv", "bin", "python")
		pipPath = filepath.Join(wd, "venv", "bin", "pip")
	}
	return pythonPath, pipPath
}

func autoManagedVenv() (string, string) {
	pythonPath, pipPath := getVenvPaths()

	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		log.Println("[Sandbox] No local sandbox detected, creating virtual environment (venv) for the first time...")
		startTime := time.Now()

		globalPy := "python"
		if runtime.GOOS != "windows" {
			globalPy = "python3"
		}
		initCmd := exec.Command(globalPy, "-m", "venv", "venv")
		initCmd.Stdout = os.Stdout
		initCmd.Stderr = os.Stderr

		if err := initCmd.Run(); err != nil {
			log.Fatalf("[Sandbox] Failed to create sandbox: %v", err)
		}
		log.Printf("[Sandbox] Sandbox environment created successfully! Duration: %v\n", time.Since(startTime))
	}

	log.Println("[Sandbox] Checking/installing dependencies in sandbox environment...")
	pipStart := time.Now()

	installCmd := exec.Command(pipPath, "install", "-r", "requirements.txt")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		log.Printf("[Sandbox] Dependency sync may have warnings: %v\n", err)
	} else {
		log.Printf("[Sandbox] Sandbox dependencies synced successfully! Duration: %v\n", time.Since(pipStart))
	}

	return pythonPath, pipPath
}

func StartPythonServiceAsync() {
	venvPythonPath, _ := autoManagedVenv()
	log.Println("[Background] Starting Python service asynchronously...")


	globalPythonCmd = exec.Command(venvPythonPath, "sensevoice.py", "--port", fmt.Sprintf("%d", config.SenseVoicePort))

	prepareCmdAttr(globalPythonCmd)


	globalPythonCmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	globalPythonCmd.Stdout = os.Stdout
	globalPythonCmd.Stderr = os.Stderr

	if err := globalPythonCmd.Start(); err != nil {
		log.Fatalf("[Background] Failed to start Python process: %v", err)
	}

	go func() {
		log.Println("[Background] Python service starting silently in background...")

		
		_ = globalPythonCmd.Wait()
		log.Println("[Background] Python process has exited.")
	}()
}
