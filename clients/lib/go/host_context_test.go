package directions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostContextOptions(t *testing.T) {
	root := repoFixture(t)
	put(t, root, ".directions/base.toml", strings.ReplaceAll(contextConfig, "profiles.default", "profiles.review"))
	put(t, root, "src/original.go", "locally dirty but not authoritative")
	paths := []string{"docs/start.md"}
	result, err := BuildContext(context.Background(), root, ContextOptions{
		ExactRoot: true, IncludeAgents: true, Profile: "review", ChangedPaths: &paths,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "profiles.review") || !strings.Contains(result.Content, "DOC_RULE") || strings.Contains(result.Content, "SOURCE_RULE") {
		t.Fatal(result.Content)
	}
	if len(result.Agents) != 1 || result.Agents[0].Content != "NATIVE_AGENTS_CONTEXT" {
		t.Fatal(result.Agents)
	}
	put(t, root, "AGENTS.override.md", "OVERRIDE_CONTEXT")
	paths = []string{}
	result, err = BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, IncludeAgents: true, Profile: "review", ChangedPaths: &paths})
	if err != nil || len(result.Warnings) != 0 || result.Agents[0].Content != "OVERRIDE_CONTEXT" || strings.Contains(result.Content, "DOC_RULE") {
		t.Fatal(result, err)
	}
	for _, name := range []string{"/outside", "src/../outside", "src/..", "../outside", "."} {
		paths = []string{name}
		if _, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, Profile: "review", ChangedPaths: &paths}); err == nil {
			t.Fatalf("invalid host path accepted: %q", name)
		}
	}
}

func TestExactRootWithoutDirectionsAndAgentsConfinement(t *testing.T) {
	parent := repoFixture(t)
	root := filepath.Join(parent, "child")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	put(t, root, "AGENTS.md", "CHILD_CONTEXT")
	result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, IncludeAgents: true})
	if err != nil || result.Content != "" || result.Agents[0].Content != "CHILD_CONTEXT" {
		t.Fatal(result, err)
	}
	if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(parent, "AGENTS.md"), filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, IncludeAgents: true}); err == nil {
		t.Fatal("outside-root AGENTS symlink was read")
	}
}
