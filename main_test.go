// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mainArgs carries the arguments of a dvpnd run from a test to the child it
// starts, separated by newlines.
const mainArgs = "DVPND_TEST_MAIN_ARGS"

// TestMain lets a test run dvpnd itself: started with mainArgs set, the test
// binary runs main with those arguments and exits as the Go runtime does when
// main returns.
func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(mainArgs); ok {
		os.Args = append([]string{"dvpnd"}, strings.Split(args, "\n")...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runDvpnd runs dvpnd with args in a child process and returns its exit
// status and output.
func runDvpnd(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), mainArgs+"="+strings.Join(args, "\n"), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exit):
		return exit.ExitCode(), string(out)
	default:
		t.Fatalf("running dvpnd %v: %v", args, err)
		return 0, ""
	}
}

// TestFailingCommandExitsNonZero holds dvpnd to the exit status a failure
// must have: the installer runs under set -e, and systemd and Docker read the
// status. A command that succeeds is the control: the child runs dvpnd.
//
// Rules: [RT-10].
func TestFailingCommandExitsNonZero(t *testing.T) {
	if status, out := runDvpnd(t, "version"); status != 0 {
		t.Fatalf("dvpnd version exited %d:\n%s", status, out)
	}

	home := t.TempDir() // no config.toml in it
	for _, args := range [][]string{
		{"start", "--home", home},
		{"config", "set", "node.moniker", "--home", home},
	} {
		status, out := runDvpnd(t, args...)
		if !strings.Contains(out, "Error:") {
			t.Errorf("dvpnd %v did not fail as expected; output:\n%s", args, out)
		}
		if status == 0 {
			t.Errorf("dvpnd %v failed (%s) yet exited 0", args, strings.TrimSpace(out))
		}
	}
}
