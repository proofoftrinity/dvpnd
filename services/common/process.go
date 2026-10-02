// SPDX-License-Identifier: Apache-2.0

package common

import (
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// verbose says whether the protocol daemons may log per-connection detail:
// client addresses and, for the proxies, the destinations clients reach. It
// follows the node's own log level, so a node at the default level keeps
// neither in its journal; an operator chasing a fault runs the node with
// --log_level debug and gets the daemons' usual output back.
var verbose atomic.Bool

// SetVerbose is called once by the start command, before the service writes
// its daemon's configuration.
func SetVerbose(v bool) { verbose.Store(v) }

// Verbose reports what SetVerbose stored.
func Verbose() bool { return verbose.Load() }

// Process is a protocol's child process (a proxy or VPN daemon) with the
// lifecycle every service needs: start, reap on exit, stop politely.
type Process struct {
	cmd    *exec.Cmd
	exited chan error // receives the child's Wait result once it has exited
	onStop func()     // runs once the child has exited after Stop
}

// StartProcess launches name with args; extraEnv is appended to the current
// environment and attr, when set, says which account the child runs as. The
// child's output goes to the node's stdout and stderr, and a goroutine reaps
// it whenever it exits so a crash leaves no zombie.
func StartProcess(name string, args []string, extraEnv []string, attr *syscall.SysProcAttr) (*Process, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = attr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	p := &Process{cmd: cmd, exited: make(chan error, 1)}
	go func() { p.exited <- cmd.Wait() }()

	return p, nil
}

// Stop sends SIGTERM and waits for the child, killing it after timeout. An
// exit status reported after the signal is expected and not an error.
func (p *Process) Stop(timeout time.Duration) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return errors.New("process was not started")
	}
	if p.onStop != nil {
		defer p.onStop()
	}

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	select {
	case <-p.exited:
		return nil
	case <-time.After(timeout):
	}

	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-p.exited

	return nil
}

// Exited reports whether the child has exited, and how.
func (p *Process) Exited() (bool, *os.ProcessState) {
	if p == nil || p.cmd == nil || p.cmd.ProcessState == nil {
		return false, nil
	}

	return true, p.cmd.ProcessState
}
