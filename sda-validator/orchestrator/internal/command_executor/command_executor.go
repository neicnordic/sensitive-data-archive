package command_executor

import "os/exec"

// CommandExecutor is an interface to execute commands towards the os
type CommandExecutor interface {
	Execute(name string, args ...string) ([]byte, error)
}

type OsCommandExecutor struct {
}

func (OsCommandExecutor) Execute(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
