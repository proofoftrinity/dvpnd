// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"text/template"
)

// WritePrivateFile writes data to path readable by its owner only. The node's
// configuration files hold keys and passwords, and a file written by an
// earlier release with wider permissions is narrowed too.
func WritePrivateFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}

	return os.Chmod(path, 0o600)
}

// TOMLString renders v as a quoted TOML basic string. A JSON string is one
// (its escapes are a subset of TOML's), so a value holding a quote or a
// newline stays one value instead of breaking the file or adding keys to it.
func TOMLString(v interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fmt.Sprint(v)); err != nil {
		panic(err) // a string always encodes
	}

	return string(bytes.TrimRight(buf.Bytes(), "\n"))
}

// ConfigTemplate parses a configuration file template with the "toml"
// function, which every string value goes through.
func ConfigTemplate(name, text string) *template.Template {
	return template.Must(template.New(name).Funcs(template.FuncMap{"toml": TOMLString}).Parse(text))
}
