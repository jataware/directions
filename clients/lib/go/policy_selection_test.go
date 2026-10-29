package directions

import (
	"context"
	"strings"
	"testing"
)

const selectionPolicies = `
[policies.code.always]
title = "Always"
instructions = "CODE_ALWAYS"
[policies.code.source]
title = "Source"
instructions = "CODE_SOURCE"
triggers.diff = ["src/**/*.go"]
[policies.docs.style]
title = "Docs"
instructions = "DOCS_STYLE"
`

func selectionConfig(selection string) string {
	return minimal + "[profiles.default]\n" + selection + selectionPolicies
}

func TestPolicySelectionModes(t *testing.T) {
	for _, tc := range []struct {
		name, selection string
		want            []string
	}{
		{"default all", "", []string{"CODE_ALWAYS", "CODE_SOURCE", "DOCS_STYLE"}},
		{"whitelist group", "[[profiles.default.whitelist]]\ngroup=\"code\"\n", []string{"CODE_ALWAYS", "CODE_SOURCE"}},
		{"whitelist policy", "[[profiles.default.whitelist]]\ngroup=\"code\"\npolicy=\"source\"\n", []string{"CODE_SOURCE"}},
		{"blacklist group", "[[profiles.default.blacklist]]\ngroup=\"code\"\n", []string{"DOCS_STYLE"}},
		{"blacklist policy", "[[profiles.default.blacklist]]\ngroup=\"code\"\npolicy=\"source\"\n", []string{"CODE_ALWAYS", "DOCS_STYLE"}},
		{"empty whitelist", "whitelist=[]\n", nil},
		{"empty blacklist", "blacklist=[]\n", []string{"CODE_ALWAYS", "CODE_SOURCE", "DOCS_STYLE"}},
		{"duplicate selectors", "whitelist=[{group=\"code\"},{group=\"code\",policy=\"always\"}]\n", []string{"CODE_ALWAYS", "CODE_SOURCE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, ".directions/base.toml", selectionConfig(tc.selection))
			paths := []string{"src/a.go"}
			result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, ChangedPaths: &paths})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"CODE_ALWAYS", "CODE_SOURCE", "DOCS_STYLE"} {
				want := false
				for _, match := range tc.want {
					if match == name {
						want = true
					}
				}
				count := strings.Count(result.Content, name)
				if (count == 1) != want || count > 1 {
					t.Fatalf("%s: count=%d, want included=%v\n%s", name, count, want, result.Content)
				}
			}
		})
	}
}

func TestPolicySelectionStillHonorsTriggers(t *testing.T) {
	for _, selection := range []string{"", "whitelist=[{group=\"code\"}]\n", "blacklist=[{group=\"docs\"}]\n"} {
		root := t.TempDir()
		put(t, root, ".directions/base.toml", selectionConfig(selection))
		paths := []string{"docs/readme.md"}
		result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, ChangedPaths: &paths})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Content, "CODE_ALWAYS") || strings.Contains(result.Content, "CODE_SOURCE") {
			t.Fatal(result.Content)
		}
	}
}

func TestPolicySelectionValidation(t *testing.T) {
	for _, selection := range []string{
		"whitelist=[]\nblacklist=[]\n",
		"whitelist=[{group=\"code\"}]\nblacklist=[]\n",
		"blacklist=[{group=\"code\"}]\nwhitelist=[]\n",
		"whitelist=[{policy=\"always\"}]\n",
		"blacklist=[{group=\"code\",policy=\"\"}]\n",
		"whitelist=[{group=\"code\",unknown=\"x\"}]\n",
		"policies=[]\n",
	} {
		if _, err := Parse([]byte(selectionConfig(selection))); err == nil {
			t.Fatalf("invalid selection accepted: %s", selection)
		}
	}
	if _, err := Parse([]byte(minimal + "[profile.default]\n")); err == nil {
		t.Fatal("legacy singular table accepted")
	}
	for _, mode := range []string{"whitelist", "blacklist"} {
		for _, ref := range []string{"{group=\"missing\"}", "{group=\"code\",policy=\"missing\"}"} {
			root := t.TempDir()
			put(t, root, ".directions/base.toml", selectionConfig(mode+"=["+ref+"]\n"))
			if _, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true}); err == nil {
				t.Fatalf("unknown %s reference accepted: %s", mode, ref)
			}
		}
	}
}

func TestPolicySelectionComposition(t *testing.T) {
	for _, tc := range []struct {
		name, baseSelection, fragment string
		wantError                     bool
		wantNew                       bool
	}{
		{"new policies join defaults", "", "[policies.extra.new]\ntitle=\"New\"\ninstructions=\"NEW_POLICY\"", false, true},
		{"whitelist excludes new groups", "whitelist=[{group=\"code\"}]\n", "[policies.extra.new]\ntitle=\"New\"\ninstructions=\"NEW_POLICY\"", false, false},
		{"blacklist includes new groups", "blacklist=[{group=\"code\"}]\n", "[policies.extra.new]\ntitle=\"New\"\ninstructions=\"NEW_POLICY\"", false, true},
		{"whitelist mode in fragment", "", "[profiles.default]\nwhitelist=[]", false, false},
		{"cross-file conflict", "whitelist=[]\n", "[profiles.default]\nblacklist=[]", true, false},
		{"reverse cross-file conflict", "blacklist=[]\n", "[profiles.default]\nwhitelist=[]", true, false},
		{"single-fragment conflict", "", "[profiles.default]\nwhitelist=[]\nblacklist=[]", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, ".directions/base.toml", selectionConfig(tc.baseSelection)+"\n[[overrides]]\npath=\".directions/fragment.toml\"\n")
			put(t, root, ".directions/fragment.toml", tc.fragment)
			paths := []string{}
			result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, ChangedPaths: &paths})
			if tc.wantError {
				if err == nil {
					t.Fatal("conflict accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.Content, "NEW_POLICY") != tc.wantNew {
				t.Fatal(result.Content)
			}
			if tc.name == "whitelist mode in fragment" && strings.Contains(result.Content, "CODE_ALWAYS") {
				t.Fatal("empty fragment whitelist lost during merge")
			}
		})
	}
}

func TestNoDiffNeededForExcludedPolicies(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".directions/base.toml", selectionConfig("whitelist=[]\n"))
	result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true})
	if err != nil || len(result.Warnings) != 0 {
		t.Fatal(result, err)
	}
}
