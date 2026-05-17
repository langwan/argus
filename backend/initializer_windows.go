package main

import (
	"os/exec"
	"syscall"
)

func prepareCmdAttr(cmd *exec.Cmd) {

	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
