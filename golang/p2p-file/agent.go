package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

type agentManager struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

func newAgentManager() *agentManager {
	return &agentManager{}
}

func (a *agentManager) Launch(storageDir string) error {
	if storageDir == "" {
		return fmt.Errorf("storage directory not configured")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--agent", "--storage", storageDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	a.cmd = cmd
	go func() {
		_ = cmd.Wait()
		a.mu.Lock()
		if a.cmd == cmd {
			a.cmd = nil
		}
		a.mu.Unlock()
	}()
	return nil
}

func (a *agentManager) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd == nil || a.cmd.Process == nil || a.cmd.ProcessState != nil {
		return nil
	}
	if err := a.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	go func(cmd *exec.Cmd) {
		_ = cmd.Wait()
	}(a.cmd)
	a.cmd = nil
	return nil
}

func (a *agentManager) Status() (bool, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd == nil || a.cmd.Process == nil || a.cmd.ProcessState != nil {
		return false, 0
	}
	return true, a.cmd.Process.Pid
}
