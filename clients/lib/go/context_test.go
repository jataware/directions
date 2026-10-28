package directions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, data string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

const contextConfig = `meta.version = "v1.0.0"
[repo]
vcs = "git"
[profiles.default]
prompt = "contexts/general.md"
[[profiles.default.whitelist]]
group = "rules"
[policies.rules.always]
title = "Always"
instructions = "ALWAYS_RULE"
[policies.rules.source]
title = "Source"
instructions = "SOURCE_RULE"
triggers.diff = ["src/**/*.go"]
[policies.rules.docs]
title = "Docs"
instructions = "DOC_RULE"
triggers.diff = ["docs/*.md"]
`

func repoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "user.email", "test@example.invalid")
	git(t, root, "config", "user.name", "Test")
	put(t, root, ".directions/base.toml", contextConfig)
	put(t, root, ".directions/contexts/general.md", "GENERAL_CONTEXT")
	put(t, root, "AGENTS.md", "NATIVE_AGENTS_CONTEXT")
	put(t, root, "src/original.go", "original")
	put(t, root, "docs/start.md", "original")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "initial")
	return root
}

func load(t *testing.T, cwd string) *ContextResult {
	t.Helper()
	result, err := InitialContext(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInitialContextRefresh(t *testing.T) {
	root := repoFixture(t)
	clean := load(t, root)
	if !strings.Contains(clean.Content, "GENERAL_CONTEXT") || !strings.Contains(clean.Content, "ALWAYS_RULE") ||
		strings.Contains(clean.Content, "SOURCE_RULE") || strings.Contains(clean.Content, "DOC_RULE") ||
		strings.Contains(clean.Content, "NATIVE_AGENTS_CONTEXT") {
		t.Fatal(clean.Content)
	}
	put(t, root, "src/original.go", "unstaged")
	put(t, root, "docs/start.md", "staged")
	git(t, root, "add", "docs/start.md")
	changed := load(t, filepath.Join(root, "src"))
	if changed.Root != root || !strings.Contains(changed.Content, "SOURCE_RULE") || !strings.Contains(changed.Content, "DOC_RULE") {
		t.Fatal(changed)
	}
	git(t, root, "reset", "--hard", "HEAD")
	put(t, root, "src/new\nfile.go", "untracked")
	if !strings.Contains(load(t, root).Content, "SOURCE_RULE") {
		t.Fatal("untracked file did not trigger")
	}
	if err := os.Remove(filepath.Join(root, "src/new\nfile.go")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "mv", "src/original.go", "renamed.txt")
	if !strings.Contains(load(t, root).Content, "SOURCE_RULE") {
		t.Fatal("rename source did not trigger")
	}
	git(t, root, "reset", "--hard", "HEAD")
	git(t, root, "rm", "docs/start.md")
	if !strings.Contains(load(t, root).Content, "DOC_RULE") {
		t.Fatal("deletion did not trigger")
	}
}

func TestUnbornAndUnavailableDiff(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	put(t, root, ".directions/base.toml", contextConfig)
	put(t, root, ".directions/contexts/general.md", "GENERAL_CONTEXT")
	put(t, root, "src/new.go", "new")
	result := load(t, root)
	if len(result.Warnings) != 0 || !strings.Contains(result.Content, "SOURCE_RULE") {
		t.Fatal(result)
	}
	// Host Git routing environment must not redirect inspection to another repo.
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "missing"))
	if len(load(t, root).Warnings) != 0 {
		t.Fatal("inherited GIT_DIR was used")
	}
	put(t, root, ".directions/base.toml", strings.Replace(contextConfig, `vcs = "git"`, `vcs = "none"`, 1))
	result = load(t, root)
	if !strings.Contains(result.Content, "DOC_RULE") || !strings.Contains(result.Content, "applicability: unknown") || len(result.Warnings) == 0 {
		t.Fatal(result)
	}
	put(t, root, ".directions/base.toml", contextConfig)
	t.Setenv("PATH", t.TempDir())
	if !strings.Contains(load(t, root).Content, "applicability: unknown") {
		t.Fatal("missing Git silently skipped policies")
	}
}

func TestCompositionAndProvenance(t *testing.T) {
	root := repoFixture(t)
	put(t, root, ".directions/base.toml", contextConfig+"\n[[overrides]]\npath.rel=\"policies/*.toml\"\n")
	put(t, root, ".directions/policies/01.toml", "[profiles.default]\nprompt=\"nested.md\"\n[policies.rules.always]\ninstructions=\"OVERRIDE_ONE\"\n")
	put(t, root, ".directions/policies/nested.md", "NESTED_CONTEXT")
	put(t, root, ".directions/policies/02.toml", "[policies.rules.always]\ninstructions=\"OVERRIDE_TWO\"\n")
	result := load(t, root)
	if !strings.Contains(result.Content, "OVERRIDE_TWO") || strings.Contains(result.Content, "OVERRIDE_ONE") || !strings.Contains(result.Content, "NESTED_CONTEXT") {
		t.Fatal(result)
	}
	// Arrays replace rather than append, and duplicate selections are deduplicated.
	put(t, root, ".directions/policies/03.toml", "[profiles.default]\nwhitelist=[{group=\"rules\",policy=\"always\"},{group=\"rules\",policy=\"always\"}]\n")
	if strings.Count(load(t, root).Content, "OVERRIDE_TWO") != 1 {
		t.Fatal("duplicate policy context")
	}
}

func TestContextErrors(t *testing.T) {
	for name, config := range map[string]string{
		"missing profile":          minimal,
		"unknown group":            strings.Replace(contextConfig, `group = "rules"`, `group = "missing"`, 1),
		"unknown policy":           strings.Replace(contextConfig, `group = "rules"`, "group = \"rules\"\npolicy=\"missing\"", 1),
		"home prompt":              strings.Replace(contextConfig, `prompt = "contexts/general.md"`, `prompt.home = "secret"`, 1),
		"root prompt":              strings.Replace(contextConfig, `prompt = "contexts/general.md"`, `prompt.root = "/etc/passwd"`, 1),
		"invalid glob":             strings.Replace(contextConfig, "src/**/*.go", "src/[.go", 1),
		"embedded doublestar":      strings.Replace(contextConfig, "src/**/*.go", "src/**.go", 1),
		"literal missing override": contextConfig + "\n[[overrides]]\npath.rel=\"missing.toml\"",
		"home override":            contextConfig + "\n[[overrides]]\npath.home=\"*.toml\"",
	} {
		t.Run(name, func(t *testing.T) {
			root := repoFixture(t)
			put(t, root, ".directions/base.toml", config)
			if _, err := InitialContext(context.Background(), root); err == nil {
				t.Fatal("expected context error")
			}
		})
	}
	for name, fragment := range map[string]string{
		"nested":               "[[overrides]]\npath.rel=\"other.toml\"",
		"version":              "meta.version=\"v2.0.0\"",
		"vcs":                  "[repo]\nvcs=\"none\"",
		"unknown key":          "[polcies.rules.test]\ntitle=\"bad\"",
		"invalid final policy": "[policies.rules.new]\ntitle=\"missing instructions\"",
	} {
		t.Run(name, func(t *testing.T) {
			root := repoFixture(t)
			put(t, root, ".directions/base.toml", contextConfig+"\n[[overrides]]\npath.rel=\"fragment.toml\"")
			put(t, root, ".directions/fragment.toml", fragment)
			if _, err := InitialContext(context.Background(), root); err == nil {
				t.Fatal("expected fragment error")
			}
		})
	}
}

func TestConfinementAndLimits(t *testing.T) {
	root := repoFixture(t)
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("DO_NOT_READ"), 0600); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(root, ".directions/contexts/general.md")
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, prompt); err != nil {
		t.Fatal(err)
	}
	if _, err := InitialContext(context.Background(), root); err == nil {
		t.Fatal("read outside root through symlink")
	}
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	put(t, root, ".directions/contexts/general.md", strings.Repeat("x", maxContextBytes+1))
	if _, err := InitialContext(context.Background(), root); err == nil {
		t.Fatal("oversized context accepted")
	}
	put(t, root, ".directions/contexts/general.md", "GENERAL")
	put(t, root, ".directions/base.toml", contextConfig+"\n[[overrides]]\npath.rel=\"policies/*.toml\"")
	// An unmatched glob is valid, including a nonexistent static directory prefix.
	load(t, root)
	for i := 0; i < maxOverrideFiles+1; i++ {
		put(t, root, ".directions/policies/"+strings.Repeat("x", i+1)+".toml", "")
	}
	if _, err := InitialContext(context.Background(), root); err == nil {
		t.Fatal("override file cap ignored")
	}
}

func TestDiscoveryBoundary(t *testing.T) {
	root := repoFixture(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q")
	result := load(t, nested)
	if result.Root != "" || result.Content != "" {
		t.Fatal("crossed nested repository boundary")
	}
}

func TestGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"src/**/*.go", "src/a.go", true}, {"src/**/*.go", "src/a/b.go", true},
		{"src/*.go", "src/a/b.go", false}, {"**/test?.[ch]", "test1.c", true},
		{"**/test?.[ch]", "deep/test1.h", true}, {"**/test?.[ch]", "deep/test11.h", false},
		{"**/x/**/y", "x/y", true}, {"*.md", "README.MD", false},
	} {
		if got := globMatch(tc.pattern, tc.name); got != tc.want {
			t.Fatalf("%q %q: %v", tc.pattern, tc.name, got)
		}
	}
}
