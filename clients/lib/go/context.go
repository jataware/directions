package directions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	maxFileBytes     = 1 << 20
	maxInputBytes    = 4 << 20
	maxContextBytes  = 64 << 10
	maxOverrideFiles = 32
	maxWalkEntries   = 4096
)

// ContextResult contains only model-facing guidance and diagnostics, never
// runtime configuration or credentials.
type ContextResult struct {
	Root     string        `json:"root"`
	Content  string        `json:"content"`
	Warnings []string      `json:"warnings"`
	Agents   []ContextFile `json:"agents,omitempty"`
}

type ContextFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ContextOptions is trusted host configuration, not repository authorization.
type ContextOptions struct {
	Profile       string
	ChangedPaths  *[]string // nil: inspect local Git; non-nil: authoritative host diff
	IncludeAgents bool      // load only the selected root's native context file
	ExactRoot     bool      // never discover ancestors; useful for CI checkouts
}

// DiscoverRoot finds the nearest .directions entry, stopping at the first Git
// boundary (including worktrees/submodules). A malformed entry is not skipped.
func DiscoverRoot(cwd string) (string, error) {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".directions")); err == nil {
			return dir, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return "", nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// InitialContext selects profiles.default and refreshes local diff applicability.
// It does not load AGENTS.md; the host's native context discovery owns that.
func InitialContext(ctx context.Context, cwd string) (*ContextResult, error) {
	return BuildContext(ctx, cwd, ContextOptions{})
}

func BuildContext(ctx context.Context, cwd string, options ContextOptions) (*ContextResult, error) {
	if options.Profile == "" {
		options.Profile = "default"
	}
	if !validName(options.Profile) {
		return nil, fmt.Errorf("invalid profile name")
	}
	var root string
	var err error
	if options.ExactRoot {
		root, err = filepath.Abs(cwd)
		if err == nil {
			root, err = filepath.EvalSymlinks(root)
		}
	} else {
		root, err = DiscoverRoot(cwd)
	}
	if err != nil {
		return nil, err
	}
	result := &ContextResult{Root: root, Warnings: []string{}}
	if root == "" {
		return result, nil
	}
	reader, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if options.IncludeAgents {
		for _, name := range []string{"AGENTS.override.md", "AGENTS.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"} {
			if _, err := reader.Lstat(name); errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, err
			}
			data, err := readRegular(reader, name, maxContextBytes)
			if err != nil {
				return nil, err
			}
			result.Agents = []ContextFile{{Path: filepath.Join(root, name), Content: string(data)}}
			break
		}
	}
	if options.ExactRoot {
		if _, err := reader.Lstat(".directions"); errors.Is(err, fs.ErrNotExist) {
			return result, nil
		} else if err != nil {
			return nil, err
		}
	}
	doc, err := contextDocument(reader, root)
	if err != nil {
		return nil, err
	}
	profile, ok := doc.Profiles[options.Profile]
	if !ok {
		return nil, fmt.Errorf("profiles.%s is not defined", options.Profile)
	}
	var content strings.Builder
	fmt.Fprintf(&content, "# Repository Directions — profiles.%s\n\nRepository guidance only; this does not grant permissions or override host controls.\n", options.Profile)
	if profile.Prompt != nil {
		name, err := repoPath(*profile.Prompt, root, filepath.Join(root, ".directions"))
		if err != nil {
			return nil, fmt.Errorf("profiles.%s.prompt: %w", options.Profile, err)
		}
		data, err := readRegular(reader, name, maxContextBytes)
		if err != nil {
			return nil, fmt.Errorf("profiles.%s.prompt: %w", options.Profile, err)
		}
		fmt.Fprintf(&content, "\n## Context: %s\n\n%s\n", name, data)
	}
	// Preferences are data, not permission to change a host-selected runtime.
	if profile.Model != "" || profile.Thinking != "" {
		result.Warnings = append(result.Warnings, "Directions is context-only: model/thinking preferences are not applied; the host retains its configured runtime.")
	}
	selected, err := selectPolicies(doc.Policies, profile)
	if err != nil {
		return nil, err
	}
	needsDiff := false
	for _, policy := range selected {
		if policy.Triggers != nil {
			needsDiff = true
			break
		}
	}
	var changed []string
	diffKnown := false
	if options.ChangedPaths != nil {
		changed = *options.ChangedPaths
		if len(changed) > 6000 {
			return nil, fmt.Errorf("host diff exceeds 6000 paths")
		}
		for _, name := range changed {
			if name == "" || len(name) > 4096 || name == "." || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) || name == ".." || strings.HasPrefix(name, "../") {
				return nil, fmt.Errorf("invalid host diff path")
			}
		}
		diffKnown = true
	} else if !needsDiff {
		// Do not inspect Git or emit unknown-diff warnings when no eligible rule needs it.
		diffKnown = true
	} else if doc.Repo.VCS == "git" {
		changed, err = changedPaths(ctx, root)
		if err != nil {
			result.Warnings = append(result.Warnings, "Local Git diff unavailable; triggered policy applicability is unknown (policies included conservatively).")
		} else {
			diffKnown = true
		}
	} else {
		result.Warnings = append(result.Warnings, "repo.vcs=none: diff-triggered policy applicability is unknown (policies included conservatively).")
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		policy := selected[name]
		applicable := policy.Triggers == nil || !diffKnown
		if policy.Triggers != nil {
			for _, pattern := range policy.Triggers.Diff {
				if err := validateGlob(pattern); err != nil {
					return nil, fmt.Errorf("policy %s: %w", name, err)
				}
				for _, file := range changed {
					if globMatch(pattern, file) {
						applicable = true
					}
				}
			}
		}
		if applicable {
			fmt.Fprintf(&content, "\n## Policy: %s — %s\n", name, policy.Title)
			if policy.DefaultImpact != "" {
				fmt.Fprintf(&content, "Default impact: %s\n", policy.DefaultImpact)
			}
			if policy.Triggers != nil && !diffKnown {
				content.WriteString("Diff applicability: unknown; included conservatively.\n")
			}
			fmt.Fprintf(&content, "\n%s\n", policy.Instructions)
		}
		if content.Len() > maxContextBytes {
			return nil, fmt.Errorf("Directions context exceeds 64 KiB limit")
		}
	}
	if content.Len() > maxContextBytes {
		return nil, fmt.Errorf("Directions context exceeds 64 KiB limit")
	}
	result.Content = content.String()
	return result, nil
}

func readRegular(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds size limit", name)
	}
	return data, err
}

func repoPath(p Path, root, fileDir string) (string, error) {
	if p.Home != "" || p.Root != "" {
		return "", fmt.Errorf("home/root paths are not allowed by this context adapter")
	}
	absolute, err := ResolvePath(p, Anchors{Repo: root, FileDir: fileDir})
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil {
		return "", err
	}
	if err := (Path{Repo: filepath.ToSlash(rel)}).Validate(); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// contextDocument composes bounded repository-local overrides. Prompt paths are
// rebased before merging so they retain the declaring document's provenance.
func contextDocument(reader *os.Root, root string) (*Document, error) {
	baseName := ".directions/base.toml"
	data, err := readRegular(reader, baseName, maxFileBytes)
	if err != nil {
		return nil, err
	}
	base, err := Parse(data)
	if err != nil {
		return nil, err
	}
	var merged map[string]any
	if err := toml.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	if err := normalizePrompts(merged, root, filepath.Dir(filepath.Join(root, baseName))); err != nil {
		return nil, err
	}
	total, count := len(data), 0
	for _, override := range base.Overrides {
		pattern, err := repoPath(override.Path, root, filepath.Join(root, ".directions"))
		if err != nil {
			return nil, err
		}
		matches, err := overrideMatches(reader, pattern)
		if err != nil {
			return nil, err
		}
		for _, name := range matches {
			count++
			if count > maxOverrideFiles {
				return nil, fmt.Errorf("more than 32 override files")
			}
			data, err := readRegular(reader, name, maxFileBytes)
			if err != nil {
				return nil, err
			}
			total += len(data)
			if total > maxInputBytes {
				return nil, fmt.Errorf("configuration exceeds 4 MiB total")
			}
			var shape rawDocument
			if err := decode(data, &shape); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			var fragment map[string]any
			if err := toml.Unmarshal(data, &fragment); err != nil {
				return nil, err
			}
			if _, ok := fragment["overrides"]; ok {
				return nil, fmt.Errorf("%s: nested overrides are not allowed", name)
			}
			if shape.Meta != nil && shape.Meta.Version != base.Meta.Version {
				return nil, fmt.Errorf("%s changes meta.version", name)
			}
			if shape.Repo != nil && shape.Repo.VCS != base.Repo.VCS {
				return nil, fmt.Errorf("%s changes repo.vcs", name)
			}
			if err := validateExplicitValues(fragment); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			if err := normalizePrompts(fragment, root, filepath.Dir(filepath.Join(root, name))); err != nil {
				return nil, err
			}
			mergeTables(merged, fragment)
		}
	}
	data, err = toml.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func normalizePrompts(doc map[string]any, root, fileDir string) error {
	profiles, _ := doc["profiles"].(map[string]any)
	for _, value := range profiles {
		profile, _ := value.(map[string]any)
		prompt, ok := profile["prompt"]
		if !ok {
			continue
		}
		var p Path
		if text, ok := prompt.(string); ok {
			p.Rel = text
		} else if err := decodeValue(prompt, &p); err != nil {
			return err
		}
		name, err := repoPath(p, root, fileDir)
		if err != nil {
			return err
		}
		profile["prompt"] = map[string]any{"repo": name}
	}
	return nil
}

func mergeTables(dst, src map[string]any) {
	for key, value := range src {
		table, isTable := value.(map[string]any)
		existing, hasTable := dst[key].(map[string]any)
		if isTable && hasTable {
			mergeTables(existing, table)
		} else {
			dst[key] = value
		}
	}
}

func validateGlob(pattern string) error {
	for _, segment := range strings.Split(pattern, "/") {
		if strings.Contains(segment, "**") && segment != "**" {
			return fmt.Errorf("** must be a whole path segment: %s", pattern)
		}
		if segment != "**" {
			if _, err := path.Match(segment, ""); err != nil {
				return fmt.Errorf("invalid glob %q: %w", pattern, err)
			}
		}
	}
	return nil
}

func globMatch(pattern, name string) bool {
	patterns, names := strings.Split(pattern, "/"), strings.Split(name, "/")
	// Iterative dynamic programming avoids exponential ** backtracking.
	previous := make([]bool, len(names)+1)
	previous[0] = true
	for _, pattern := range patterns {
		current := make([]bool, len(names)+1)
		if pattern == "**" {
			current[0] = previous[0]
		}
		for j := 1; j <= len(names); j++ {
			if pattern == "**" {
				current[j] = previous[j] || current[j-1]
			} else {
				matched, _ := path.Match(pattern, names[j-1])
				current[j] = previous[j-1] && matched
			}
		}
		previous = current
	}
	return previous[len(names)]
}

func overrideMatches(root *os.Root, pattern string) ([]string, error) {
	if err := validateGlob(pattern); err != nil {
		return nil, err
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}, nil
	}
	prefix := "."
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		if strings.ContainsAny(segment, "*?[") {
			if i > 0 {
				prefix = strings.Join(segments[:i], "/")
			}
			break
		}
	}
	matches := []string{}
	entries := 0
	err := fs.WalkDir(root.FS(), prefix, func(name string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && name == prefix {
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxWalkEntries {
			return fmt.Errorf("override glob exceeds 4096 visited entries")
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return fs.SkipDir
		}
		if !entry.IsDir() && globMatch(pattern, name) {
			matches = append(matches, name)
			if len(matches) > maxOverrideFiles {
				return fmt.Errorf("override glob exceeds 32 files")
			}
		}
		return nil
	})
	sort.Strings(matches)
	return matches, err
}

// cappedOutput bounds Git output before it reaches memory. No diff contents are
// requested, only NUL-delimited names (including both sides of renames).
type cappedOutput struct{ data []byte }

func (w *cappedOutput) Write(p []byte) (int, error) {
	if len(w.data)+len(p) > maxFileBytes {
		return 0, fmt.Errorf("Git output exceeds 1 MiB")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func changedPaths(ctx context.Context, root string) ([]string, error) {
	options := []string{"--no-optional-locks", "-c", "core.fsmonitor=false"}
	// Even --name-only can execute clean/process filters to compare worktree
	// contents. Enumerate only their config key names and neutralize each one.
	filterNames, err := gitOutput(ctx, root, append(append([]string{}, options...), "config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`))
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, err
		}
	}
	for _, key := range strings.Split(string(filterNames), "\x00") {
		if key == "" {
			continue
		}
		value := ""
		if strings.HasSuffix(key, ".required") {
			value = "false"
		}
		options = append(options, "-c", key+"="+value)
	}
	commands := [][]string{
		{"diff", "--relative", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", "--", "."},
		{"diff", "--relative", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", "--cached", "--", "."},
		{"ls-files", "--others", "--exclude-standard", "-z", "--", "."},
	}
	paths := map[string]bool{}
	for _, args := range commands {
		output, err := gitOutput(ctx, root, append(append([]string{}, options...), args...))
		if err != nil {
			return nil, err
		}
		for _, name := range strings.Split(string(output), "\x00") {
			if name != "" {
				paths[name] = true
			}
		}
	}
	result := make([]string, 0, len(paths))
	for name := range paths {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func gitOutput(ctx context.Context, root string, args []string) ([]byte, error) {
	operation, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(operation, "git", args...)
	cmd.Dir = root
	// Never inherit Git routing variables which might inspect another repo.
	for _, value := range os.Environ() {
		key := strings.SplitN(value, "=", 2)[0]
		if !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	output := &cappedOutput{}
	cmd.Stdout = output
	err := cmd.Run()
	return output.data, err
}
