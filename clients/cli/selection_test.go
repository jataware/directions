package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExplainUsesSameSelectorWithoutInstructionBodies(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".directions"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		".directions/base.toml": "meta.version=\"v1.0.0\"\n[repo]\nvcs=\"none\"\n[profiles.default]\n",
		"REVIEWER.md":           "SECRET_INSTRUCTION_BODY",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	diff := filepath.Join(t.TempDir(), "diff.json")
	if err := os.WriteFile(diff, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	results := map[string]map[string]any{}
	for _, command := range []string{"context", "explain"} {
		var out, errs bytes.Buffer
		if code := run([]string{command, root, "--exact-root", "--operation", "review", "--path", "src/caller.go", "--diff-file", diff}, &out, &errs); code != 0 {
			t.Fatal(code, errs.String())
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		results[command] = result
		if command == "explain" && (strings.Contains(out.String(), "SECRET_INSTRUCTION_BODY") || result["content"] != nil || result["agents"] != nil) {
			t.Fatal(out.String())
		}
	}
	if !reflect.DeepEqual(results["context"]["decisions"], results["explain"]["decisions"]) {
		t.Fatal(results)
	}
}
