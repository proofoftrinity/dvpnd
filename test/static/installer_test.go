// SPDX-License-Identifier: Apache-2.0

package static

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// aptGetRun matches apt-get run as a command, not named in a check for it.
var aptGetRun = regexp.MustCompile(`(^|[^-\w])apt-get\s`)

// fakeAptGet logs its arguments and answers as apt does while another run
// holds its locks: update finds the package lists' lock taken twice, install
// gets the dpkg lock only when told to wait for it, and a package that does
// not exist fails the way it always will.
const fakeAptGet = `#!/bin/sh
echo "$*" >>"$LOG"
case " $* " in
  *" nosuchpackage "*) echo "E: Unable to locate package nosuchpackage" >&2; exit 100 ;;
  *" update "*) [ "$(wc -l <"$LOG")" -gt 2 ] || { echo "E: Could not get lock /var/lib/apt/lists/lock" >&2; exit 100; } ;;
  *" install "*) case "$*" in *"-o DPkg::Lock::Timeout="*) ;; *) echo "E: Could not get lock /var/lib/dpkg/lock-frontend" >&2; exit 100 ;; esac ;;
esac
`

// TestInstallerWaitsForThePackageLock holds the installer to waiting while
// another apt run holds the package manager, as unattended-upgrades does in a
// fresh server's first minutes: every apt-get goes through apt_get, which
// waits for the dpkg lock and retries update, since apt itself waits for the
// dpkg lock only. The wrapper runs here against a fake apt-get.
//
// Rules: [RT-11].
func TestInstallerWaitsForThePackageLock(t *testing.T) {
	const file = "scripts/install.sh"
	lines := strings.Split(string(read(t, file)), "\n")
	start, end := -1, -1
	for i, line := range lines {
		if line == "apt_get() {" {
			start = i
		} else if start >= 0 && line == "}" {
			end = i
			break
		}
	}

	runs := 0
	for i, line := range lines {
		code, _, _ := strings.Cut(line, "#")
		if !aptGetRun.MatchString(code) || strings.Contains(code, "command -v apt-get") {
			continue
		}
		runs++
		if start < 0 || i < start || i > end {
			t.Errorf("%s:%d runs apt-get itself; use apt_get, which waits while another apt run holds the lock: %s",
				file, i+1, strings.TrimSpace(line))
		}
	}
	if runs == 0 {
		t.Fatalf("%s runs no apt-get; this test reads the wrong file", file)
	}
	if start < 0 || end < 0 {
		t.Fatalf("%s defines no apt_get() { ... } function to run apt-get through", file)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "apt-get"), []byte(fakeAptGet), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(dir, "log")
	wrapper := strings.Join(lines[start:end+1], "\n")
	run := func(args string) error {
		script := "set -Eeuo pipefail\nsleep() { :; }\n" + wrapper + "\napt_get " + args + "\n"
		cmd := exec.Command("bash", "-c", script)
		cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "LOG="+logFile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("apt_get %s: %v\n%s", args, err, out)
		}

		return err
	}

	if err := run("update -qq"); err != nil {
		t.Error("apt_get update gave up while the package lists' lock was taken")
	}
	if err := run("install -y -qq ufw"); err != nil {
		t.Error("apt_get install did not wait for the dpkg lock")
	}
	if err := run("install -y -qq nosuchpackage"); err == nil {
		t.Error("apt_get install reported a package that does not exist as installed")
	}
	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(logged)), "\n")
	if len(calls) != 5 {
		t.Errorf("apt-get ran %d times, want 5 (update three times, then each install once):\n%s", len(calls), strings.Join(calls, "\n"))
	}
}

// shellFunction returns the lines of the function name defines in a shell
// script, from "name() {" to its closing "}", or nothing if it has none.
func shellFunction(lines []string, name string) (start, end int) {
	start, end = -1, -1
	for i, line := range lines {
		if line == name+"() {" {
			start = i
		} else if start >= 0 && line == "}" {
			return start, i
		}
	}

	return -1, -1
}

// TestInstallerTrustsTheSourceOnce holds the installer to one safe.directory
// entry for the tree it builds, however often it runs: an upgrade is a re-run,
// and each run used to add another copy to root's git configuration. A run
// also folds the copies earlier runs left into one, and leaves the operator's
// own entries alone. trust_source runs here against a git configuration of
// the test's own.
//
// Rules: [RT-12].
func TestInstallerTrustsTheSourceOnce(t *testing.T) {
	const file = "scripts/install.sh"
	lines := strings.Split(string(read(t, file)), "\n")
	start, end := shellFunction(lines, "trust_source")
	if start < 0 {
		t.Fatalf("%s defines no trust_source() { ... } function to add the safe.directory entry", file)
	}
	for i, line := range lines {
		if code, _, _ := strings.Cut(line, "#"); strings.Contains(code, "safe.directory") && (i < start || i > end) {
			t.Errorf("%s:%d changes safe.directory outside trust_source: %s", file, i+1, strings.TrimSpace(line))
		}
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.tree")
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := filepath.Join(dir, "gitconfig")
	env := append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+dir, "GIT_CONFIG_GLOBAL="+gitConfig,
		"GIT_CONFIG_NOSYSTEM=1", "SRC="+src)
	trustSource := strings.Join(lines[start:end+1], "\n")
	run := func() {
		cmd := exec.Command("bash", "-c", "set -Eeuo pipefail\n"+trustSource+"\ntrust_source\n")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("trust_source: %v\n%s", err, out)
		}
	}
	entries := func() []string {
		cmd := exec.Command("git", "config", "--global", "--get-all", "safe.directory")
		cmd.Env = env
		out, _ := cmd.Output()

		return strings.Fields(string(out))
	}

	run()
	run()
	if got := entries(); len(got) != 1 || got[0] != src {
		t.Errorf("after two runs from no entry, safe.directory is %q, want only %q", got, src)
	}

	// The operator's entries, one of which only a pattern would take for src,
	// and two copies of src that earlier runs left.
	other, nearMiss := filepath.Join(dir, "other"), filepath.Join(dir, "srcXtree")
	seeded := "[safe]\n\tdirectory = " + other + "\n\tdirectory = " + src + "\n\tdirectory = " + nearMiss +
		"\n\tdirectory = " + src + "\n"
	if err := os.WriteFile(gitConfig, []byte(seeded), 0o600); err != nil {
		t.Fatal(err)
	}
	run()
	got, want := entries(), []string{other, src, nearMiss}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("after a run over two copies, safe.directory is %q, want %q in any order", got, want)
	}
}
