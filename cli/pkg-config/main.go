// Command pkg-config is a minimal stand-in for pkg-config, enough for cgo to build miqt
// (the Qt binding of the Qt window, cmd/go-upnp-player-qt) against an installed Qt SDK
// with nothing else installed. A design-time tool: it is used only to build the Qt window.
//
// It answers what cgo asks: --cflags and --libs for a list of packages, following their
// Requires. The .pc files Qt ships name the prefix of Qt's own build machine
// (/Users/qt/work/install); the prefix used here is where the .pc file lies: <prefix>/lib/pkgconfig.
//
//	go build -o bin/pkg-config.exe ./cli/pkg-config
//	set PKG_CONFIG=<repo>\bin\pkg-config.exe
//	set PKG_CONFIG_PATH=C:\Qt\6.8.3\mingw_64\lib\pkgconfig
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// pc is one .pc file: its variables and fields, with ${var} not yet expanded.
type pc struct {
	vars   map[string]string
	fields map[string]string
}

var varRef = regexp.MustCompile(`\$\{(\w+)\}`)

func main() {
	var cflags, libs, modversion, exists bool
	var names []string
	for _, a := range os.Args[1:] {
		switch a {
		case "--cflags":
			cflags = true
		case "--libs":
			libs = true
		case "--modversion":
			modversion = true
		case "--exists":
			exists = true
		case "--":
		default:
			if strings.HasPrefix(a, "--") {
				continue // options cgo does not depend on
			}
			names = append(names, a)
		}
	}
	dirs := filepath.SplitList(os.Getenv("PKG_CONFIG_PATH"))
	var out []string
	seen := map[string]bool{}
	add := func(flags string) {
		for _, f := range strings.Fields(flags) {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	visited := map[string]bool{}
	var walk func(name string) error
	walk = func(name string) error {
		if visited[name] {
			return nil
		}
		visited[name] = true
		p, err := load(dirs, name)
		if err != nil {
			return err
		}
		if cflags {
			add(p.get("Cflags"))
		}
		if libs {
			add(p.get("Libs"))
		}
		for _, r := range strings.FieldsFunc(p.get("Requires"), func(r rune) bool { return r == ' ' || r == ',' }) {
			if err := walk(r); err != nil {
				return err
			}
		}
		return nil
	}
	for _, n := range names {
		if modversion {
			p, err := load(dirs, n)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println(p.get("Version"))
		}
		if err := walk(n); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if exists || modversion {
		return
	}
	fmt.Println(strings.Join(out, " "))
}

// load reads name.pc from the first directory that has it; its prefix is where it lies.
func load(dirs []string, name string) (pc, error) {
	for _, d := range dirs {
		path := filepath.Join(d, name+".pc")
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		defer func() { _ = f.Close() }()
		p := pc{vars: map[string]string{}, fields: map[string]string{}}
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if i := strings.IndexAny(line, ":="); i > 0 {
				k, v := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
				if line[i] == '=' {
					p.vars[k] = v
				} else {
					p.fields[k] = v
				}
			}
		}
		prefix, _ := filepath.Abs(filepath.Join(d, "..", ".."))
		p.vars["prefix"] = filepath.ToSlash(prefix)
		return p, s.Err()
	}
	return pc{}, fmt.Errorf("package %s was not found in PKG_CONFIG_PATH %q", name, os.Getenv("PKG_CONFIG_PATH"))
}

// get is a field with its ${var} references expanded.
func (p pc) get(field string) string {
	v := p.fields[field]
	for range 10 { // variables refer to one another a few levels deep
		next := varRef.ReplaceAllStringFunc(v, func(m string) string { return p.vars[varRef.FindStringSubmatch(m)[1]] })
		if next == v {
			break
		}
		v = next
	}
	return v
}
