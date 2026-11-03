package directions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// Decision explains selection without duplicating instruction bodies. Sources
// list contributing documents in composition order (not just the last override).
type Decision struct {
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	Sources      []string `json:"sources"`
	Scope        []string `json:"scope"`
	MatchedPaths []string `json:"matched_paths,omitempty"`
	Included     bool     `json:"included"`
	Reason       string   `json:"reason"`
}

func recordPolicySources(dst map[string][]string, doc *Document, source string) {
	for group, policies := range doc.Policies {
		for name := range policies {
			key := group + "." + name
			if !contains(dst[key], source) {
				dst[key] = append(dst[key], source)
			}
		}
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func normalizedPaths(paths []string) ([]string, error) {
	if len(paths) > 6000 {
		return nil, fmt.Errorf("scope exceeds 6000 paths")
	}
	unique := map[string]bool{}
	for _, name := range paths {
		if name == "" || len(name) > 4096 || name == "." || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") || name == ".." || strings.HasPrefix(name, "../") || name == ".git" || strings.HasPrefix(name, ".git/") {
			return nil, fmt.Errorf("invalid scope path")
		}
		unique[name] = true
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func assembleSelection(reader *os.Root, doc *Document, eligible map[string]Policy, provenance map[string][]string, options ContextOptions, changed []string, known bool, result *ContextResult, content *strings.Builder) error {
	names := []string{}
	for group, policies := range doc.Policies {
		for name := range policies {
			names = append(names, group+"."+name)
		}
	}
	sort.Strings(names)
	diagnosticBytes := 0
	for _, name := range names {
		group, key, _ := strings.Cut(name, ".")
		policy := doc.Policies[group][key]
		decision := Decision{Kind: "policy", ID: name, Sources: provenance[name], Scope: []string{"**"}}
		if policy.Triggers != nil {
			decision.Scope = policy.Triggers.Diff
			for _, pattern := range decision.Scope {
				if err := validateGlob(pattern); err != nil {
					return fmt.Errorf("policy %s: %w", name, err)
				}
			}
			for _, file := range changed {
				for _, pattern := range decision.Scope {
					if globMatch(pattern, file) {
						decision.MatchedPaths = append(decision.MatchedPaths, file)
						break
					}
				}
			}
		}
		_, allowed := eligible[name]
		switch {
		case !allowed:
			decision.Reason = "excluded by profile"
		case len(policy.Operations) > 0 && !contains(policy.Operations, options.Operation):
			decision.Reason = "operation does not match"
		case policy.Triggers == nil:
			decision.Included = true
			decision.Reason = "unconditional policy"
		case !known:
			decision.Included = true
			decision.Reason = "diff unknown; included conservatively"
		case len(decision.MatchedPaths) > 0:
			decision.Included = true
			decision.Reason = "diff scope matches"
		default:
			decision.Reason = "no changed or additional path matches"
		}
		encoded, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		diagnosticBytes += len(encoded)
		if diagnosticBytes > 512<<10 {
			return fmt.Errorf("selection diagnostics exceed 512 KiB limit")
		}
		result.Decisions = append(result.Decisions, decision)
		if decision.Included {
			fmt.Fprintf(content, "\n## Policy: %s — %s\nSources: %s\nScope: %s\nSelection: %s\n", name, policy.Title, strings.Join(decision.Sources, ", "), strings.Join(decision.Scope, ", "), decision.Reason)
			if policy.DefaultImpact != "" {
				fmt.Fprintf(content, "Default impact: %s\n", policy.DefaultImpact)
			}
			if policy.Triggers != nil && !known {
				content.WriteString("Diff applicability: unknown; included conservatively.\n")
			}
			fmt.Fprintf(content, "\n%s\n", policy.Instructions)
		}
		if content.Len() > maxContextBytes {
			return fmt.Errorf("Directions context exceeds 64 KiB limit")
		}
	}
	if options.Operation == "review" {
		return appendReviewers(reader, changed, known, result, content, diagnosticBytes)
	}
	return nil
}

// Walk only ancestors of changed paths, never the whole source tree. Missing
// directories are allowed for deletions. Symlink directories and nested Git
// roots are boundaries, including when their targets are inside the snapshot.
func appendReviewers(reader *os.Root, changed []string, known bool, result *ContextResult, content *strings.Builder, diagnosticBytes int) error {
	candidates := map[string]bool{"REVIEWER.md": true}
	visited := map[string]bool{}
	blocked := map[string]bool{}
	for _, file := range changed {
		dir := path.Dir(file)
		parts := strings.Split(dir, "/")
		prefix := ""
		for _, part := range parts {
			if part == "." {
				continue
			}
			prefix = path.Join(prefix, part)
			if !visited[prefix] {
				visited[prefix] = true
				if len(visited) > maxWalkEntries {
					return fmt.Errorf("review guidance exceeds 4096 directories")
				}
				info, err := reader.Lstat(prefix)
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
					blocked[prefix] = true
				}
				if !blocked[prefix] {
					if _, err := reader.Lstat(path.Join(prefix, ".git")); err == nil {
						blocked[prefix] = true
					} else if !errors.Is(err, fs.ErrNotExist) {
						return err
					}
				}
			}
			if blocked[prefix] {
				break
			}
			candidates[path.Join(prefix, "REVIEWER.md")] = true
		}
	}
	if !known {
		result.Warnings = append(result.Warnings, "Review diff unknown: only root and explicitly scoped REVIEWER.md files can be discovered; directory guidance coverage is incomplete.")
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)
	count := 0
	for _, name := range names {
		if _, err := reader.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		count++
		if count > maxOverrideFiles {
			return fmt.Errorf("review guidance exceeds 32 files")
		}
		data, err := readRegular(reader, name, maxContextBytes)
		if err != nil {
			return err
		}
		scope := "**"
		dir := path.Dir(name)
		if dir != "." {
			// Directory names are literals, not user-authored glob patterns.
			scope = strings.NewReplacer("*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`).Replace(dir) + "/**"
		}
		decision := Decision{Kind: "reviewer", ID: name, Sources: []string{name}, Scope: []string{scope}, Included: true, Reason: "directory ancestor of changed or additional path"}
		if dir == "." {
			decision.Reason = "repository-wide review guidance"
		}
		for _, file := range changed {
			if dir == "." || strings.HasPrefix(file, dir+"/") {
				decision.MatchedPaths = append(decision.MatchedPaths, file)
			}
		}
		encoded, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		diagnosticBytes += len(encoded)
		if diagnosticBytes > 512<<10 {
			return fmt.Errorf("selection diagnostics exceed 512 KiB limit")
		}
		result.Decisions = append(result.Decisions, decision)
		fmt.Fprintf(content, "\n## Review guidance: %s\nSource: %s\nScope: %s\n\n%s\n", name, name, scope, data)
		if content.Len() > maxContextBytes {
			return fmt.Errorf("Directions context exceeds 64 KiB limit")
		}
	}
	return nil
}
