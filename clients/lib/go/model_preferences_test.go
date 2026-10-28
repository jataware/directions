package directions

import (
	"context"
	"strings"
	"testing"
)

func TestOptionalModelPreferences(t *testing.T) {
	for _, fields := range []string{
		"", "thinking=\"medium\"",
		"model=\"openai/gpt-6.1-sol\"",
		"model=\"openrouter/organization/model-name\"\nthinking=\"medium\"",
	} {
		if _, err := Parse([]byte(minimal + "[profiles.default]\n" + fields)); err != nil {
			t.Fatal(fields, err)
		}
	}
	for _, model := range []string{"", "plain-model", "openai/", "/model", "OpenAI/model", "openai/model name", "openai/ model", "openai/model\u2003name"} {
		data := minimal + "[profiles.default]\nmodel=\"" + model + "\""
		if _, err := Parse([]byte(data)); err == nil {
			t.Fatalf("invalid model accepted: %q", model)
		}
	}
}

func TestProviderConfigurationIsRejected(t *testing.T) {
	for _, data := range []string{
		"[providers]",
		"[providers.openai]\nslug=\"openai\"\napi-type=\"openai-responses\"",
		"[[providers.openai.auth]]\ntype=\"oauth\"\npath.home=\"credentials.json\"",
		"[profiles.default]\nprovider=\"openai\"\nmodel=\"openai/gpt-6.1-sol\"",
		"[profiles.default]\nauth=\"credentials.json\"",
	} {
		if _, err := Parse([]byte(minimal + data)); err == nil {
			t.Fatal("provider config accepted", data)
		}
		if _, err := ParseFragment([]byte(data)); err == nil {
			t.Fatal("provider config accepted in fragment", data)
		}
	}
	root := t.TempDir()
	put(t, root, ".directions/base.toml", minimal+"[profiles.default]\n[[overrides]]\npath.rel=\"provider.toml\"")
	put(t, root, ".directions/provider.toml", "[providers.openai]")
	if _, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true}); err == nil {
		t.Fatal("provider config loaded through override")
	}
}

func TestModelHintsStayOutOfContext(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".directions/base.toml", minimal+"[profiles.default]\nmodel=\"custom/private-model-preference\"\nthinking=\"medium\"")
	result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "private-model-preference") || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "model/thinking preferences") {
		t.Fatal(result)
	}
}
