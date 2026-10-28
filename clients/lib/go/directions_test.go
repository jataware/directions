package directions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimal = "meta.version = \"v1.0.0\"\n[repo]\nvcs = \"none\"\n"

func TestStarter(t *testing.T) {
	data, err := os.ReadFile("../../../.directions/base.toml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Profiles["default"].Prompt.Rel != "contexts/general.md" ||
		doc.Profiles["default"].Model != "openai/gpt-6.1-sol" ||
		doc.Policies["meta"]["only-prompts-in-contexts-dir"].DefaultImpact != "blocking" {
		t.Fatalf("unexpected parsed starter: %#v", doc)
	}
}

func TestInvalidDocuments(t *testing.T) {
	tests := map[string]string{
		"empty model":           minimal + "[profiles.default]\nmodel=\"\"",
		"empty typed anchor":    minimal + "[profiles.default.prompt]\nrel=\"\"\nrepo=\"x\"",
		"empty override anchor": minimal + "[[overrides]]\npath.rel=\"\"\npath.repo=\"x\"",
		"empty policy selector": minimal + "[[profiles.default.whitelist]]\ngroup=\"meta\"\npolicy=\"\"",
		"empty impact":          minimal + "[policies.meta.test]\ntitle=\"Test\"\ninstructions=\"Do this\"\ndefault-impact=\"\"",
		"missing version":       "[repo]\nvcs=\"git\"",
		"missing repo":          "meta.version=\"v1.0.0\"",
		"version":               strings.Replace(minimal, "v1.0.0", "v2.0.0", 1),
		"vcs":                   strings.Replace(minimal, "none", "svn", 1),
		"unknown":               minimal + "[typo]\nx=1",
		"unknown profile key":   minimal + "[profiles.default]\nmodle=\"x\"",
		"duplicate table":       minimal + "[repo]\nvcs=\"git\"",
		"unquoted version":      "meta.version=v1.0.0",
		"unqualified model":     minimal + "[profiles.default]\nmodel=\"gpt-6.1-sol\"",
		"bad profile name":      minimal + "[profiles.Default]",
		"invalid prompt type":   minimal + "[profiles.default]\nprompt=2",
		"prompt escape":         minimal + "[profiles.default]\nprompt=\"../outside.md\"",
		"empty prompt":          minimal + "[profiles.default]\nprompt=\"\"",
		"prompt unknown key":    minimal + "[profiles.default.prompt]\nother=\"x\"",
		"multiple anchors":      minimal + "[profiles.default.prompt]\nrel=\"x\"\nhome=\"y\"",
		"empty selector":        minimal + "[[profiles.default.whitelist]]\npolicy=\"x\"",
		"missing instructions":  minimal + "[policies.meta.test]\ntitle=\"Test\"",
		"invalid impact":        minimal + "[policies.meta.test]\ntitle=\"Test\"\ninstructions=\"Do this\"\ndefault-impact=\"urgent\"",
		"empty trigger":         minimal + "[policies.meta.test]\ntitle=\"Test\"\ninstructions=\"Do this\"\ntriggers.diff=[]",
		"invalid trigger":       minimal + "[policies.meta.test]\ntitle=\"Test\"\ninstructions=\"Do this\"\ntriggers.diff=[\"../x\"]",
		"empty override":        minimal + "[[overrides]]",
		"unknown override":      minimal + "[[overrides]]\npath.bad=\"x\"",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(data)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFragmentAndTypedPrompt(t *testing.T) {
	if _, err := ParseFragment([]byte("[policies.meta.test]\ntitle=\"Test\"\ninstructions=\"Do this\"")); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFragment([]byte("[[overrides]]\npath.rel=\"x.toml\"")); err == nil {
		t.Fatal("nested overrides accepted")
	}
	doc, err := Parse([]byte(minimal + "[profiles.default.prompt]\nrepo=\"docs/context.md\""))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Profiles["default"].Prompt.Repo != "docs/context.md" {
		t.Fatal("typed prompt lost")
	}
}

func TestResolvePaths(t *testing.T) {
	base := t.TempDir()
	anchors := Anchors{FileDir: filepath.Join(base, ".directions"), Repo: base, Home: filepath.Join(base, "home")}
	for _, tc := range []struct {
		path Path
		want string
	}{
		{Path{Rel: "contexts/a.md"}, filepath.Join(anchors.FileDir, "contexts/a.md")},
		{Path{Repo: ".directions/policies/*.toml"}, filepath.Join(base, ".directions/policies/*.toml")},
		{Path{Home: ".config/directions/local.toml"}, filepath.Join(anchors.Home, ".config/directions/local.toml")},
		{Path{Root: filepath.Join(base, "absolute")}, filepath.Join(base, "absolute")},
	} {
		got, err := ResolvePath(tc.path, anchors)
		if err != nil || got != tc.want {
			t.Fatalf("%#v: %q, %v; want %q", tc.path, got, err, tc.want)
		}
	}
	for _, path := range []Path{
		{}, {Rel: "x", Repo: "y"}, {Rel: "../outside"}, {Rel: "a/../../outside"},
		{Home: "/absolute"}, {Root: "relative"}, {Repo: "~/x"}, {Rel: "x\x00"},
		{Rel: "C:/x"}, {Rel: `a\b`}, {Rel: " "},
	} {
		if _, err := ResolvePath(path, anchors); err == nil {
			t.Fatalf("accepted %#v", path)
		}
	}
	if _, err := ResolvePath(Path{Rel: "x"}, Anchors{}); err == nil {
		t.Fatal("missing anchor accepted")
	}
	// Resolution must not require the target to exist or read it.
	if _, err := ResolvePath(Path{Home: "does-not-exist"}, anchors); err != nil {
		t.Fatal(err)
	}
}
