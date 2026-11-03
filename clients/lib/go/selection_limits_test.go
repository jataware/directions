package directions

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReviewerLiteralDirectoryScope(t *testing.T) {
	root := selectionFixture(t)
	put(t, root, "src/[api]/REVIEWER.md", "LITERAL_DIRECTORY")
	paths := []string{"src/[api]/file.go"}
	result, err := BuildContext(context.Background(), root, ContextOptions{Operation: "review", ChangedPaths: &paths})
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range result.Decisions {
		if decision.ID == "src/[api]/REVIEWER.md" {
			if len(decision.MatchedPaths) != 1 || !globMatch(decision.Scope[0], paths[0]) {
				t.Fatal(decision)
			}
			return
		}
	}
	t.Fatal("literal directory guidance missing")
}

func TestDiagnosticsBoundedAndUnknownReviewScope(t *testing.T) {
	root := selectionFixture(t)
	result, err := BuildContext(context.Background(), root, ContextOptions{Operation: "review"})
	if err != nil || result.DiffKnown || !strings.Contains(result.Content, "ROOT_REVIEW") || strings.Contains(result.Content, "SOURCE_REVIEW") {
		t.Fatal(result, err)
	}
	paths := make([]string, 6000)
	for i := range paths {
		paths[i] = fmt.Sprintf("src/%s%d.go", strings.Repeat("x", 100), i)
	}
	_, err = BuildContext(context.Background(), root, ContextOptions{Operation: "review", ChangedPaths: &paths})
	if err == nil || !strings.Contains(err.Error(), "diagnostics exceed") {
		t.Fatal(err)
	}
}
