//go:build sdarwin || linux

package main

import (
	"os/exec"
	"syscall"
)

func prepareCmdAttr(cmd *exec.Cmd) {

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
