// SPDX-License-Identifier: Apache-2.0

package static

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// licenceMarkers are the titles and opening phrases of licence texts, and
// whether a dependency may carry that licence. As in the CI licence gate:
// permissive, public-domain and weak-copyleft licences may; strong copyleft,
// network copyleft and source-available ones may not.
var licenceMarkers = []struct {
	marker, name string
	allowed      bool
}{
	{"Apache License", "Apache-2.0", true},
	{"Permission is hereby granted, free of charge", "MIT", true},
	{"Redistribution and use in source and binary forms", "BSD", true},
	{"Permission to use, copy, modify, and/or distribute this software for any purpose", "ISC", true},
	{"Permission to use, copy, modify, and distribute this software for any purpose", "ISC", true},
	{"Boost Software License", "BSL-1.0", true},
	{"This software is provided 'as-is', without any express or implied warranty", "Zlib", true},
	{"This is free and unencumbered software released into the public domain", "Unlicense", true},
	{"CC0 1.0 Universal", "CC0-1.0", true},
	{"Mozilla Public License", "MPL", true},
	{"Eclipse Public License", "EPL", true},
	{"GNU Affero General Public License", "AGPL", false},
	{"GNU General Public License", "GPL", false},
	{"GNU Lesser General Public License", "LGPL", false},
	{"GNU Library General Public License", "LGPL", false},
	{"Server Side Public License", "SSPL", false},
	{"Business Source License", "BUSL", false},
	{"Commons Clause", "Commons Clause", false},
	{"Elastic License", "Elastic", false},
	{"European Union Public Licence", "EUPL", false},
	{"Creative Commons Attribution-NonCommercial", "CC-BY-NC", false},
	{"Creative Commons Attribution-ShareAlike", "CC-BY-SA", false},
}

// licenceFile matches the names a licence text goes by.
var licenceFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|unlicense)`)

// classify names the licence a text carries: the one whose marker comes first,
// since a licence names others after its own title (MPL-2.0 names the GNU
// licences it is compatible with). ok is false when no marker is found.
func classify(text string) (name string, allowed, ok bool) {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	first := -1
	for _, m := range licenceMarkers {
		if i := strings.Index(text, strings.ToLower(m.marker)); i >= 0 && (first < 0 || i < first) {
			first, name, allowed, ok = i, m.name, m.allowed, true
		}
	}

	return name, allowed, ok
}

// TestClassify holds the classifier to known texts, so a classifier that
// recognises nothing, or everything, cannot pass the test below.
func TestClassify(t *testing.T) {
	for _, c := range []struct {
		text, name  string
		allowed, ok bool
	}{
		{"MIT License\n\nPermission is hereby granted, free of charge, to any person", "MIT", true, true},
		{"                    GNU GENERAL PUBLIC LICENSE\n                       Version 3", "GPL", false, true},
		{"GNU LESSER GENERAL PUBLIC LICENSE Version 2.1 ... the GNU General Public License", "LGPL", false, true},
		{"Mozilla Public License Version 2.0 ... GNU General Public License, Version 2.0, the GNU Lesser General Public License", "MPL", true, true},
		{"Copyright 2024 Someone. All rights reserved.", "", false, false},
	} {
		name, allowed, ok := classify(c.text)
		if name != c.name || allowed != c.allowed || ok != c.ok {
			t.Errorf("classify(%.40q) = %q, %v, %v; want %q, %v, %v", c.text, name, allowed, ok, c.name, c.allowed, c.ok)
		}
	}
}

// TestEveryDependencyIsLicensed reads the licence of every package linked
// into dvpnd from the module cache, with the toolchain and the network both
// off, the way the CI licence gate does with go-licenses: the licence file
// nearest the package, up to its module's root. A package without one is all
// rights reserved.
//
// Rules: [LIC-7].
func TestEveryDependencyIsLicensed(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f",
		`{{if not .Standard}}{{with .Module}}{{if not .Main}}{{$.Dir}}|{{.Path}}|{{.Dir}}{{with .Replace}}|{{.Dir}}{{end}}{{end}}{{end}}{{end}}`,
		"./...")
	cmd.Dir = root(t)
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v (the module cache must hold the dependencies: go mod download)\n%s", err, out)
	}

	modules := map[string]bool{}
	checked := map[string]bool{} // licence directories already read
	carried := map[string]int{}  // licence files by licence, for -v
	var problems []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		pkgDir, module, modDir := fields[0], fields[1], fields[2]
		if len(fields) == 4 && fields[3] != "" {
			modDir = fields[3]
		}
		modules[module] = true

		dir, found := licenceDir(pkgDir, modDir)
		if !found {
			problems = append(problems, module+": no licence file between "+pkgDir+" and the module root, which means all rights reserved")
			continue
		}
		if checked[dir] {
			continue
		}
		checked[dir] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !licenceFile.MatchString(e.Name()) {
				continue
			}
			text, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			name, allowed, ok := classify(string(text))
			carried[name]++
			switch {
			case !ok:
				problems = append(problems, module+": "+e.Name()+" carries no licence this test recognises; if it is permissive or weak copyleft, add its marker to licenceMarkers")
			case !allowed:
				problems = append(problems, module+": "+e.Name()+" is "+name+", which a dependency may not carry")
			}
		}
	}

	if len(modules) < 50 {
		t.Fatalf("go list named %d modules; dvpnd links well over a hundred, so the listing is broken", len(modules))
	}
	t.Logf("%d modules; licence files by licence: %v", len(modules), carried)
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// licenceDir walks up from a package's directory to its module's root and
// returns the first directory holding a licence file.
func licenceDir(pkgDir, modDir string) (string, bool) {
	for dir := pkgDir; ; dir = filepath.Dir(dir) {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && licenceFile.MatchString(e.Name()) {
					return dir, true
				}
			}
		}
		if dir == modDir || !strings.HasPrefix(dir, modDir+string(filepath.Separator)) {
			return "", false
		}
	}
}
