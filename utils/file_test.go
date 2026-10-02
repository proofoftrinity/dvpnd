// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestWritePrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivateFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 600", info.Mode().Perm())
	}
}

// TestTOMLStringKeepsAValueOneValue: whatever an operator passes to
// "config set", it reads back as the same single value.
func TestTOMLStringKeepsAValueOneValue(t *testing.T) {
	for _, v := range []string{
		"plain",
		`with "quotes"`,
		"multi\nline = \"injected\"\n[section]",
		`back\slash`,
		"https://example.com/path?a=1&b=<2>",
		"tab\there",
	} {
		var buf bytes.Buffer
		tmpl := ConfigTemplate("t", "key = {{ toml .V }}\nother = 1\n")
		if err := tmpl.Execute(&buf, struct{ V string }{v}); err != nil {
			t.Fatal(err)
		}

		r := viper.New()
		r.SetConfigType("toml")
		if err := r.ReadConfig(&buf); err != nil {
			t.Fatalf("%q: rendered file does not parse: %v\n%s", v, err, buf.String())
		}
		if got := r.GetString("key"); got != v {
			t.Errorf("read back %q, want %q", got, v)
		}
		if keys := r.AllKeys(); len(keys) != 2 {
			t.Errorf("%q added keys: %v", v, keys)
		}
	}
	if got := TOMLString("a&b"); got != `"a&b"` {
		t.Errorf("TOMLString escapes HTML: %s", got)
	}
}
