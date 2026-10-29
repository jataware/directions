package directions

import (
	"os"
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
	if doc.Profiles["default"].Prompt != ".directions/contexts/general.md" ||
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
		"empty override path":   minimal + "[[overrides]]\npath=\"\"",
		"invalid override type": minimal + "[[overrides]]\npath=2",
		"insecure override":     minimal + "[[overrides]]\npath=\"http://example.invalid/policy.toml\"",
		"insecure prompt":       minimal + "[profiles.default]\nprompt=\"http://example.invalid/context.md\"",
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

func TestFragmentAndPlainPrompt(t *testing.T) {
	if _, err := ParseFragment([]byte("[policies.meta.test]\ntitle=\"Test\"\ninstructions=\"Do this\"")); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFragment([]byte("[[overrides]]\npath=\"x.toml\"")); err == nil {
		t.Fatal("nested overrides accepted")
	}
	doc, err := Parse([]byte(minimal + "[profiles.default]\nprompt=\"docs/context.md\"\n[[overrides]]\npath=\".directions/policies/*.toml\""))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Profiles["default"].Prompt != "docs/context.md" || doc.Overrides[0].Path != ".directions/policies/*.toml" {
		t.Fatal("plain path lost")
	}
}

func TestLocalPaths(t *testing.T) {
	// Validation is lexical: paths need not exist and no files are read.
	for _, value := range []string{"docs/context.md", ".directions/policies/*.toml", "./docs/context.md", "a/../docs/context.md", "a space.md", "does-not-exist"} {
		if err := validateLocalPath(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"", " ", "../outside", "a/../../outside", "/etc/passwd", "~/x", "~user/x", "x\x00", "C:/x", `a\b`, "file:///x", "https://example.org/policy.toml", "http://example.org/policy.toml"} {
		if err := validateLocalPath(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	// Reject the old object syntax rather than silently interpreting its anchor.
	for _, anchor := range []string{"rel", "repo", "home", "root"} {
		for _, field := range []string{"[profiles.default]\nprompt", "[[overrides]]\npath"} {
			if _, err := Parse([]byte(minimal + field + "={" + anchor + "=\"x\"}")); err == nil {
				t.Fatalf("accepted legacy %s object in %s", anchor, field)
			}
		}
	}
}
