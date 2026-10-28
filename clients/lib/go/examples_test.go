package directions

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubExampleProfiles(t *testing.T) {
	root, err := filepath.Abs("../../../examples/github")
	if err != nil {
		t.Fatal(err)
	}
	changed := []string{".github/workflows/review.yml"}
	for _, profile := range []string{"default", "review", "plan", "describe"} {
		result, err := BuildContext(context.Background(), root, ContextOptions{ExactRoot: true, Profile: profile, ChangedPaths: &changed})
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if !strings.Contains(result.Content, "Repository maintenance") {
			t.Fatal(profile, result.Content)
		}
		if profile != "describe" && !strings.Contains(result.Content, "automation privilege boundaries") {
			t.Fatal(profile, result.Content)
		}
	}
}
