package directions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitInspectionDoesNotRunHelpers(t *testing.T) {
	root := repoFixture(t)
	put(t, root, ".gitattributes", "*.go filter=probe\n")
	git(t, root, "add", ".gitattributes")
	git(t, root, "commit", "-qm", "attributes")
	put(t, root, "probe.sh", "#!/bin/sh\ntouch \"$PWD/helper-ran\"\ncat\n")
	script := filepath.Join(root, "probe.sh")
	if err := os.Chmod(script, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, root, "config", "filter.probe.clean", script)
	git(t, root, "config", "filter.probe.process", script)
	git(t, root, "config", "filter.probe.required", "true")
	git(t, root, "config", "core.fsmonitor", script)
	git(t, root, "config", "diff.external", script)
	put(t, root, "src/original.go", "changed source")
	result := load(t, root)
	if !strings.Contains(result.Content, "SOURCE_RULE") || len(result.Warnings) != 0 {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(root, "helper-ran")); !os.IsNotExist(err) {
		t.Fatal("Git helper executed during inspection")
	}
}
