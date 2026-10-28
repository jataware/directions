package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextFlags(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".directions"), 0700); err != nil {
		t.Fatal(err)
	}
	config := "meta.version=\"v1.0.0\"\n[repo]\nvcs=\"none\"\n[profiles.review]\n"
	if err := os.WriteFile(filepath.Join(root, ".directions/base.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("AGENTS_DATA"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := filepath.Join(t.TempDir(), "paths.json")
	if err := os.WriteFile(paths, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	code := run([]string{"context", root, "--profile", "review", "--exact-root", "--agents", "--diff-file", paths}, &out, &errs)
	if code != 0 || !strings.Contains(out.String(), "AGENTS_DATA") || !strings.Contains(out.String(), "profiles.review") {
		t.Fatal(code, out.String(), errs.String())
	}
	for _, data := range []string{"null", "{}", "[1]", "[\"../bad\"]"} {
		if err := os.WriteFile(paths, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if code := run([]string{"context", root, "--diff-file", paths}, &out, &errs); code != 1 {
			t.Fatal("invalid diff accepted", data, code)
		}
	}
	if code := run([]string{"context", root, "--unknown"}, &out, &errs); code != 2 {
		t.Fatal(code)
	}
}
