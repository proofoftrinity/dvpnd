// SPDX-License-Identifier: Apache-2.0

package static

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

const (
	spdxLine     = "// SPDX-License-Identifier: Apache-2.0"
	modifiedLine = "// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE."
	copyright    = "Copyright [2017] [Sentinel]"
	sdkModule    = "github.com/sentinel-official/sentinel-go-sdk"
	// The module path at the fork point and now; renaming it alone is not a
	// change to a file.
	upstreamModule = "github.com/sentinel-official/dvpn-node"
	forkModule     = "github.com/trinitystake/dvpnd/v9"
)

// Rules: [LIC-1].
func TestEveryGoFileStartsWithSPDX(t *testing.T) {
	for _, f := range files(t, "*.go") {
		first, _, _ := strings.Cut(string(read(t, f)), "\n")
		if first != spdxLine {
			t.Errorf("%s: the first line must be %q", f, spdxLine)
		}
	}
}

// Rules: [LIC-2].
func TestNoSentinelGoSDK(t *testing.T) {
	for _, f := range []string{"go.mod", "go.sum"} {
		if bytes.Contains(read(t, f), []byte(sdkModule)) {
			t.Errorf("%s names %s, which ships no licence (all rights reserved)", f, sdkModule)
		}
	}
	fset := token.NewFileSet()
	for _, f := range files(t, "*.go") {
		file, err := parser.ParseFile(fset, filepath.Join(root(t), f), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(path, sdkModule) {
				t.Errorf("%s imports %s", f, path)
			}
		}
	}
}

// knownUnmarked lists upstream files changed in substance that do not say so
// yet. Each entry is a known bug waiting for its fix; the test fails once an
// entry is no longer true, so the list can only shrink.
var knownUnmarked = map[string]string{
	"api/status/responses.go":      "known bug B3, fix awaiting approval",
	"libs/geoip/types/location.go": "known bug B3, fix awaiting approval",
	"types/session.go":             "known bug B3, fix awaiting approval",
	"utils/keys.go":                "known bug B3, fix awaiting approval",
}

// substance is a Go file's content as a multiset of lines, ignoring what does
// not change what the file says: the licence header lines, blank lines,
// whitespace inside a line, the order of lines, and the module path.
func substance(src string, upstream bool) map[string]int {
	lines := map[string]int{}
	for _, line := range strings.Split(src, "\n") {
		if upstream {
			line = strings.ReplaceAll(line, upstreamModule, forkModule)
		}
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || line == spdxLine || line == modifiedLine {
			continue
		}
		lines[line]++
	}

	return lines
}

func sameSubstance(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for line, n := range a {
		if b[line] != n {
			return false
		}
	}

	return true
}

// Rules: [LIC-3].
func TestChangedUpstreamFilesSayModified(t *testing.T) {
	upstream := map[string]bool{}
	for _, f := range strings.Split(string(git(t, "ls-tree", "-r", "-z", "--name-only", "fork-point")), "\x00") {
		upstream[f] = strings.HasSuffix(f, ".go")
	}
	if len(upstream) == 0 {
		t.Fatal("the fork-point tag lists no files; fetch the tags (git fetch --tags)")
	}

	for _, f := range files(t, "*.go") {
		if !upstream[f] {
			continue
		}
		current := string(read(t, f))
		changed := !sameSubstance(substance(string(git(t, "show", "fork-point:"+f)), true), substance(current, false))
		marked := strings.Contains(current, modifiedLine)
		_, known := knownUnmarked[f]
		switch {
		case changed && !marked && !known:
			t.Errorf("%s is changed from upstream: add the line %q under the SPDX line", f, modifiedLine)
		case known && (marked || !changed):
			t.Errorf("%s carries the notice now (or matches upstream): remove it from knownUnmarked", f)
		}
	}
	for f := range knownUnmarked {
		if _, err := os.Stat(filepath.Join(root(t), f)); err != nil {
			t.Errorf("knownUnmarked lists %s, which no longer exists", f)
		}
	}
}

// Rules: [LIC-4].
func TestLicenseIsTheForkPoints(t *testing.T) {
	if !bytes.Equal(read(t, "LICENSE"), git(t, "show", "fork-point:LICENSE")) {
		t.Error("LICENSE differs from the fork point's; it is never edited")
	}
	for _, f := range []string{"LICENSE", "NOTICE"} {
		if !bytes.Contains(read(t, f), []byte(copyright)) {
			t.Errorf("%s lost the line %q", f, copyright)
		}
	}
}

// TestProvenanceRecordVerifies runs the provenance script offline: a curl that
// always fails stands in for the network, so the outside attestations (the
// script reports them as info) are skipped and every check on the repository
// itself runs.
//
// Rules: [LIC-5].
func TestProvenanceRecordVerifies(t *testing.T) {
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "curl"), []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "docs/provenance/verify-fork.sh")
	cmd.Dir = root(t)
	cmd.Env = append(os.Environ(), "PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("RESULT: all checks passed")) {
		t.Fatalf("verify-fork.sh: %v\n%s", err, out)
	}
	if bytes.Contains(out, []byte("PASS  apache.org")) {
		t.Fatal("the script reached the network past the curl stub")
	}
}

// bech32Candidate matches what could be a Sentinel account, node or
// validator address; the checksum decides.
var bech32Candidate = regexp.MustCompile(`\bsent[a-z]*1[02-9ac-hj-np-z]{6,}\b`)

// addressAllowed lists addresses that may appear, each with its reason.
var addressAllowed = map[string]string{}

// addresses returns the checksum-valid 20- or 32-byte Sentinel addresses in
// text.
func addresses(text string) []string {
	var found []string
	for _, s := range bech32Candidate.FindAllString(text, -1) {
		if _, data, err := bech32.DecodeAndConvert(s); err == nil && (len(data) == 20 || len(data) == 32) {
			found = append(found, s)
		}
	}

	return found
}

// Rules: [LIC-6].
func TestNoOnChainAddresses(t *testing.T) {
	// Positive control: an address made here is found, one with a broken
	// checksum is not, so a scanner that finds nothing cannot pass.
	made, err := bech32.ConvertAndEncode("sentnode", bytes.Repeat([]byte{7}, 20))
	if err != nil {
		t.Fatal(err)
	}
	broken := made[:len(made)-1] + map[bool]string{true: "q", false: "p"}[made[len(made)-1] != 'q']
	if got := addresses("node " + made + " and " + broken); len(got) != 1 || got[0] != made {
		t.Fatalf("the scanner found %v in a text holding one valid address", got)
	}

	var hits []string
	for _, f := range files(t, ".") {
		b := read(t, f)
		if bytes.IndexByte(b, 0) >= 0 { // binary
			continue
		}
		for _, a := range addresses(string(b)) {
			if _, ok := addressAllowed[a]; !ok {
				hits = append(hits, f+": "+a)
			}
		}
	}
	sort.Strings(hits)
	for _, h := range hits {
		t.Errorf("an on-chain address in a committed file (it links the repository to a wallet): %s", h)
	}
}

// TestSubstance holds the comparison above to known changes: the licence
// header and the module rename are not a change, anything else is.
func TestSubstance(t *testing.T) {
	upstream := "package x\n\nimport \"" + upstreamModule + "/types\"\n\nfunc F() {}\n"
	for _, c := range []struct {
		name, current string
		changed       bool
	}{
		{"header and module rename only", spdxLine + "\n\npackage x\n\nimport \"" + forkModule + "/types\"\n\nfunc F() {}\n", false},
		{"reformatted", spdxLine + "\npackage x\nimport   \"" + forkModule + "/types\"\nfunc F()   {}\n", false},
		{"a changed line", spdxLine + "\n\npackage x\n\nimport \"" + forkModule + "/types\"\n\nfunc F() { G() }\n", true},
		{"an added line", spdxLine + "\n\npackage x\n\nimport \"" + forkModule + "/types\"\n\nfunc F() {}\n\nfunc G() {}\n", true},
	} {
		if got := !sameSubstance(substance(upstream, true), substance(c.current, false)); got != c.changed {
			t.Errorf("%s: changed = %v, want %v", c.name, got, c.changed)
		}
	}
}
