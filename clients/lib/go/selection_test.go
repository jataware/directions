package directions

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const scopedConfig = `meta.version="v1.0.0"
[repo]
vcs="none"
[profiles.default]
[policies.code.source]
title="Source"
instructions="SOURCE_GUIDANCE"
operations=["review"]
triggers.diff=["src/**"]
[policies.code.docs]
title="Docs"
instructions="DOC_GUIDANCE"
triggers.diff=["docs/**"]
`

func selectionFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, ".directions/base.toml", scopedConfig)
	put(t, root, "REVIEWER.md", "ROOT_REVIEW")
	put(t, root, "src/REVIEWER.md", "SOURCE_REVIEW")
	put(t, root, "src/deep/REVIEWER.md", "DEEP_REVIEW")
	put(t, root, "other/REVIEWER.md", "OTHER_REVIEW")
	return root
}

func TestScopedSelection(t *testing.T) {
	root := selectionFixture(t)
	paths := []string{"src/deep/deleted.go", "src/deep/deleted.go"}
	options := ContextOptions{ExactRoot: true, Operation: "review", ChangedPaths: &paths}
	result, err := BuildContext(context.Background(), root, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ROOT_REVIEW", "SOURCE_REVIEW", "DEEP_REVIEW", "SOURCE_GUIDANCE", "Source: src/REVIEWER.md", "Scope: src/**"} {
		if !strings.Contains(result.Content, expected) {
			t.Fatalf("missing %s: %s", expected, result.Content)
		}
	}
	for _, excluded := range []string{"OTHER_REVIEW", "DOC_GUIDANCE"} {
		if strings.Contains(result.Content, excluded) {
			t.Fatal(result.Content)
		}
	}
	if !result.DiffKnown || result.Operation != "review" {
		t.Fatal(result)
	}
	if len(result.Decisions) != 5 {
		t.Fatal(result.Decisions)
	}
	source := result.Decisions[1]
	if !source.Included || !reflect.DeepEqual(source.MatchedPaths, []string{"src/deep/deleted.go"}) || !reflect.DeepEqual(source.Sources, []string{".directions/base.toml"}) {
		t.Fatal(source)
	}
	again, err := BuildContext(context.Background(), root, options)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("selection must be stable", err)
	}
	options.Operation = "implement"
	result, err = BuildContext(context.Background(), root, options)
	if err != nil || strings.Contains(result.Content, "REVIEW") || strings.Contains(result.Content, "SOURCE_GUIDANCE") || result.Decisions[1].Reason != "operation does not match" {
		t.Fatal(result, err)
	}
	options.Operation = "review"
	paths = []string{}
	result, err = BuildContext(context.Background(), root, options)
	if err != nil || strings.Contains(result.Content, "SOURCE_REVIEW") || !strings.Contains(result.Content, "ROOT_REVIEW") {
		t.Fatal(result, err)
	}
	options.AdditionalPaths = []string{"src/deep/caller.go"}
	result, err = BuildContext(context.Background(), root, options)
	if err != nil || !strings.Contains(result.Content, "DEEP_REVIEW") {
		t.Fatal(result, err)
	}
	options.ChangedPaths = nil
	result, err = BuildContext(context.Background(), root, options)
	if err != nil || result.DiffKnown || len(result.Warnings) < 2 {
		t.Fatal(result, err)
	}
}

func TestSelectionProvenanceAndProfileExclusion(t *testing.T) {
	root := selectionFixture(t)
	put(t, root, ".directions/base.toml", scopedConfig+`[[overrides]]
path=".directions/policies.toml"
`)
	put(t, root, ".directions/policies.toml", `[profiles.default]
blacklist=[{group="code",policy="docs"}]
[policies.code.source]
instructions="OVERRIDDEN_GUIDANCE"
`)
	paths := []string{"src/file", "docs/file"}
	result, err := BuildContext(context.Background(), root, ContextOptions{Operation: "review", ChangedPaths: &paths})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].Reason != "excluded by profile" || result.Decisions[0].Included {
		t.Fatal(result.Decisions)
	}
	if !reflect.DeepEqual(result.Decisions[1].Sources, []string{".directions/base.toml", ".directions/policies.toml"}) {
		t.Fatal(result.Decisions)
	}
	if strings.Contains(result.Content, "SOURCE_GUIDANCE") || !strings.Contains(result.Content, "OVERRIDDEN_GUIDANCE") {
		t.Fatal(result.Content)
	}
}

func TestReviewerConfinementAndLimits(t *testing.T) {
	for _, mode := range []string{"escape-file", "symlink-dir", "nested-git", "oversized", "too-many"} {
		t.Run(mode, func(t *testing.T) {
			root := selectionFixture(t)
			paths := []string{"other/file"}
			wantError := false
			switch mode {
			case "escape-file":
				outside := t.TempDir()
				put(t, outside, "secret", "SECRET")
				os.Remove(filepath.Join(root, "other/REVIEWER.md"))
				if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "other/REVIEWER.md")); err != nil {
					t.Fatal(err)
				}
				wantError = true
			case "symlink-dir":
				if err := os.Symlink("other", filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
				paths = []string{"link/file"}
			case "nested-git":
				put(t, root, "other/.git", "gitdir: elsewhere")
			case "oversized":
				put(t, root, "other/REVIEWER.md", strings.Repeat("x", maxContextBytes))
				wantError = true
			case "too-many":
				dir := ""
				for i := 0; i < 33; i++ {
					dir = filepath.Join(dir, "sub")
					put(t, root, filepath.Join(dir, "REVIEWER.md"), "RULE")
				}
				paths = []string{filepath.ToSlash(filepath.Join(dir, "file"))}
				wantError = true
			}
			result, err := BuildContext(context.Background(), root, ContextOptions{Operation: "review", ChangedPaths: &paths})
			if wantError {
				if err == nil {
					t.Fatal("expected failure")
				}
				return
			}
			if err != nil || strings.Contains(result.Content, "OTHER_REVIEW") {
				t.Fatal(result, err)
			}
		})
	}
}

func TestInvalidOperationsAndScope(t *testing.T) {
	for _, value := range []string{`operations=[]`, `operations=[""]`, `operations=["Review"]`, `operations="review"`} {
		if _, err := Parse([]byte(strings.Replace(scopedConfig, `operations=["review"]`, value, 1))); err == nil {
			t.Fatal(value)
		}
	}
	root := selectionFixture(t)
	paths := []string{}
	for _, options := range []ContextOptions{
		{Operation: "bad operation"},
		{ChangedPaths: &paths, AdditionalPaths: []string{"../escape"}},
		{ChangedPaths: &paths, AdditionalPaths: []string{".git/config"}},
		{ChangedPaths: &paths, AdditionalPaths: []string{"src\\bad"}},
	} {
		if _, err := BuildContext(context.Background(), root, options); err == nil {
			t.Fatal(options)
		}
	}
}
