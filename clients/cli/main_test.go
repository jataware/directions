package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"context", root}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var response struct{ Content string }
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Content != "" {
		t.Fatal(out.String(), err)
	}
	if err := os.Mkdir(filepath.Join(root, ".directions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".directions/base.toml"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"context", root}, &out, &errOut); code != 1 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.toml")
	invalid := filepath.Join(dir, "invalid.toml")
	large := filepath.Join(dir, "large.toml")
	for path, data := range map[string]string{
		valid:   "meta.version=\"v1.0.0\"\n[repo]\nvcs=\"git\"",
		invalid: "meta.version=oops",
		large:   strings.Repeat(" ", maxDocumentBytes+1),
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 2}, {[]string{"unknown", valid}, 2},
		{[]string{"validate", valid}, 0}, {[]string{"validate", invalid}, 1},
		{[]string{"validate", large}, 1}, {[]string{"validate", filepath.Join(dir, "missing")}, 1},
		{[]string{"validate", dir}, 1},
	} {
		var out, errOut bytes.Buffer
		if code := run(tc.args, &out, &errOut); code != tc.code {
			t.Fatalf("%v: got %d, want %d: %s", tc.args, code, tc.code, errOut.String())
		}
		if tc.code == 0 && !strings.Contains(out.String(), "single file only") {
			t.Fatal("missing scope warning")
		}
	}
}
