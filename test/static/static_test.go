// SPDX-License-Identifier: Apache-2.0

// Package static holds the rules about the repository itself and the shape of
// code that a behaviour test cannot reach: what every file carries, what the
// history proves, and how the start command stops what it started. The tests
// read the tree and git; they never use the network.
package static

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// root is the repository root: this package sits two levels below it.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", dir, err)
	}

	return dir
}

// git runs git in the repository and returns its output. It never takes the
// index lock, so it cannot disturb a git command running at the same time.
func git(t *testing.T, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root(t)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}

	return out
}

// files lists the tracked files, and the untracked ones git does not ignore,
// that match the pathspecs: the tree as it will be committed.
func files(t *testing.T, pathspecs ...string) []string {
	t.Helper()
	out := git(t, append([]string{"ls-files", "-co", "--exclude-standard", "-z", "--"}, pathspecs...)...)
	var list []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root(t), f)); err == nil { // deleted but still in the index
			list = append(list, f)
		}
	}
	if len(list) == 0 {
		t.Fatalf("no files match %v: the scan would pass everything", pathspecs)
	}

	return list
}

func read(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root(t), rel))
	if err != nil {
		t.Fatal(err)
	}

	return b
}
