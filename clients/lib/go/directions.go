// Package directions parses draft Directions documents and assembles bounded,
// repository-local guidance. Parsing is pure; InitialContext reads configuration,
// prompt files, and local Git filenames, but never credentials or instructions as code.
package directions

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

const Version = "v1.0.0"

type Meta struct {
	Version string `toml:"version"`
}

type Repo struct {
	VCS string `toml:"vcs"`
}

// Path contains exactly one anchor. Resolution is lexical, not authorization.
type Path struct {
	Rel  string `toml:"rel"`
	Repo string `toml:"repo"`
	Home string `toml:"home"`
	Root string `toml:"root"`
}

type PolicyRef struct {
	Group  string `toml:"group"`
	Policy string `toml:"policy"`
}

type Profile struct {
	Prompt   *Path
	Model    string
	Thinking string
	// nil means absent; a pointer to an empty slice is an explicit empty list.
	Whitelist *[]PolicyRef
	Blacklist *[]PolicyRef
}

type Policy struct {
	Title         string `toml:"title"`
	Instructions  string `toml:"instructions"`
	DefaultImpact string `toml:"default-impact"`
	Triggers      *struct {
		Diff []string `toml:"diff"`
	} `toml:"triggers"`
}

type Override struct {
	Path Path `toml:"path"`
}

type Document struct {
	Meta      *Meta
	Repo      *Repo
	Profiles  map[string]Profile
	Policies  map[string]map[string]Policy
	Overrides []Override
}

type rawProfile struct {
	Prompt    any          `toml:"prompt"`
	Model     string       `toml:"model"`
	Thinking  string       `toml:"thinking"`
	Whitelist *[]PolicyRef `toml:"whitelist"`
	Blacklist *[]PolicyRef `toml:"blacklist"`
}

type rawDocument struct {
	Meta      *Meta                        `toml:"meta"`
	Repo      *Repo                        `toml:"repo"`
	Profiles  map[string]rawProfile        `toml:"profiles"`
	Policies  map[string]map[string]Policy `toml:"policies"`
	Overrides []Override                   `toml:"overrides"`
}

// Parse validates one base document. It does not load override files or check
// references that might be supplied by those files.
func Parse(data []byte) (*Document, error) { return parse(data, false) }

// ParseFragment permits omitted meta/repo but rejects nested overrides.
func ParseFragment(data []byte) (*Document, error) { return parse(data, true) }

func decode(data []byte, target any) error {
	return toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(target)
}

func decodeValue(value any, target any) error {
	data, err := toml.Marshal(value)
	if err != nil {
		return err
	}
	return decode(data, target)
}

func parse(data []byte, fragment bool) (*Document, error) {
	var raw rawDocument
	if err := decode(data, &raw); err != nil {
		return nil, fmt.Errorf("TOML: %w", err)
	}
	if !fragment && (raw.Meta == nil || raw.Repo == nil) {
		return nil, fmt.Errorf("base document requires meta.version and repo.vcs")
	}
	if raw.Meta != nil && raw.Meta.Version != Version {
		return nil, fmt.Errorf("unsupported meta.version %q", raw.Meta.Version)
	}
	if raw.Repo != nil && raw.Repo.VCS != "git" && raw.Repo.VCS != "none" {
		return nil, fmt.Errorf("repo.vcs must be git or none")
	}
	// Presence, even when empty, is invalid for fragments.
	var keys map[string]any
	if err := toml.Unmarshal(data, &keys); err != nil {
		return nil, err
	}
	if err := validateExplicitValues(keys); err != nil {
		return nil, err
	}
	if _, exists := keys["overrides"]; fragment && exists {
		return nil, fmt.Errorf("fragments cannot declare overrides")
	}
	doc := &Document{
		Meta: raw.Meta, Repo: raw.Repo, Policies: raw.Policies,
		Overrides: raw.Overrides, Profiles: map[string]Profile{},
	}
	for name, p := range raw.Profiles {
		if !validName(name) {
			return nil, fmt.Errorf("invalid profile name %q", name)
		}
		profile := Profile{Model: p.Model, Thinking: p.Thinking, Whitelist: p.Whitelist, Blacklist: p.Blacklist}
		if p.Whitelist != nil && p.Blacklist != nil {
			return nil, fmt.Errorf("profiles.%s cannot declare both whitelist and blacklist", name)
		}
		if p.Model != "" && !validModelRef(p.Model) {
			return nil, fmt.Errorf("profiles.%s model must be a qualified provider/model reference", name)
		}
		if p.Thinking != "" && strings.TrimSpace(p.Thinking) == "" {
			return nil, fmt.Errorf("profiles.%s has an empty thinking hint", name)
		}
		if p.Prompt != nil {
			path := Path{}
			switch value := p.Prompt.(type) {
			case string:
				path.Rel = value
			case map[string]any:
				if err := decodeValue(value, &path); err != nil {
					return nil, fmt.Errorf("profiles.%s.prompt: %w", name, err)
				}
			default:
				return nil, fmt.Errorf("profiles.%s.prompt must be a string or typed path", name)
			}
			if err := path.Validate(); err != nil {
				return nil, fmt.Errorf("profiles.%s.prompt: %w", name, err)
			}
			profile.Prompt = &path
		}
		for _, list := range []*[]PolicyRef{p.Whitelist, p.Blacklist} {
			if list == nil {
				continue
			}
			for _, ref := range *list {
				if !validName(ref.Group) || (ref.Policy != "" && !validName(ref.Policy)) {
					return nil, fmt.Errorf("profiles.%s has an invalid policy selector", name)
				}
			}
		}
		doc.Profiles[name] = profile
	}
	for group, policies := range raw.Policies {
		if !validName(group) {
			return nil, fmt.Errorf("invalid policy group %q", group)
		}
		for name, policy := range policies {
			if !validName(name) || strings.TrimSpace(policy.Title) == "" || strings.TrimSpace(policy.Instructions) == "" {
				return nil, fmt.Errorf("policies.%s.%s requires a valid name, title and instructions", group, name)
			}
			switch policy.DefaultImpact {
			case "", "low", "medium", "high", "blocking":
			default:
				return nil, fmt.Errorf("policies.%s.%s has an invalid default-impact", group, name)
			}
			if policy.Triggers != nil {
				if len(policy.Triggers.Diff) == 0 {
					return nil, fmt.Errorf("policies.%s.%s requires nonempty triggers.diff", group, name)
				}
				for _, pattern := range policy.Triggers.Diff {
					if err := (Path{Repo: pattern}).Validate(); err != nil {
						return nil, fmt.Errorf("policies.%s.%s triggers.diff: %w", group, name, err)
					}
				}
			}
		}
	}
	for i, override := range raw.Overrides {
		if err := override.Path.Validate(); err != nil {
			return nil, fmt.Errorf("overrides[%d]: %w", i, err)
		}
	}
	return doc, nil
}

// Validate explicit empty values separately: Go string fields otherwise cannot
// distinguish an omitted key from an explicitly empty string. Typed decoding
// above remains responsible for type errors and unknown keys.
func validateExplicitValues(keys map[string]any) error {
	checkStrings := func(table map[string]any, names ...string) error {
		for _, name := range names {
			if value, exists := table[name]; exists {
				if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
					return fmt.Errorf("%s must not be empty when supplied", name)
				}
			}
		}
		return nil
	}
	checkPath := func(value any) error {
		path, ok := value.(map[string]any)
		if !ok || len(path) != 1 {
			return fmt.Errorf("typed path requires exactly one key")
		}
		for _, value := range path {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("typed path requires a nonempty string")
			}
		}
		return nil
	}
	profiles, _ := keys["profiles"].(map[string]any)
	for _, value := range profiles {
		profile, _ := value.(map[string]any)
		if err := checkStrings(profile, "model", "thinking"); err != nil {
			return err
		}
		if prompt, ok := profile["prompt"].(map[string]any); ok {
			if err := checkPath(prompt); err != nil {
				return err
			}
		}
		_, whitelist := profile["whitelist"]
		_, blacklist := profile["blacklist"]
		if whitelist && blacklist {
			return fmt.Errorf("profiles cannot declare both whitelist and blacklist")
		}
		for _, key := range []string{"whitelist", "blacklist"} {
			refs, _ := profile[key].([]any)
			for _, value := range refs {
				ref, _ := value.(map[string]any)
				if err := checkStrings(ref, "policy"); err != nil {
					return err
				}
			}
		}
	}
	overrides, _ := keys["overrides"].([]any)
	for _, value := range overrides {
		override, _ := value.(map[string]any)
		if err := checkPath(override["path"]); err != nil {
			return err
		}
	}
	groups, _ := keys["policies"].(map[string]any)
	for _, value := range groups {
		policies, _ := value.(map[string]any)
		for _, value := range policies {
			policy, _ := value.(map[string]any)
			if err := checkStrings(policy, "default-impact"); err != nil {
				return err
			}
		}
	}
	return nil
}

var namePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

func validName(name string) bool { return namePattern.MatchString(name) }

func validModelRef(ref string) bool {
	provider, model, ok := strings.Cut(ref, "/")
	return ok && validName(provider) && model != "" && strings.IndexFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// Validate checks shape and lexical containment; it does not inspect a filesystem.
// Relative paths use portable forward slashes. Root paths use native absolute syntax.
func (p Path) Validate() error {
	count := 0
	for _, value := range []string{p.Rel, p.Repo, p.Home, p.Root} {
		if value != "" {
			count++
			if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
				return fmt.Errorf("path must be nonempty and contain no NUL")
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("path requires exactly one of rel, repo, home, root")
	}
	if p.Root != "" {
		if !filepath.IsAbs(p.Root) {
			return fmt.Errorf("root path must be absolute")
		}
		return nil
	}
	value := p.Rel
	if p.Repo != "" {
		value = p.Repo
	}
	if p.Home != "" {
		value = p.Home
	}
	// Reject drive, URI, tilde and backslash forms instead of interpreting them.
	if strings.ContainsAny(value, "\\:") || strings.HasPrefix(value, "~") || filepath.IsAbs(value) {
		return fmt.Errorf("relative paths must use forward slashes without absolute, tilde or URI syntax")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes its anchor")
	}
	return nil
}

// Anchors are explicit absolute directories supplied by a trusted host.
// FileDir is the directory containing the document which declared the path.
type Anchors struct {
	FileDir string
	Repo    string
	Home    string
}

func ResolvePath(p Path, anchors Anchors) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	if p.Root != "" {
		return filepath.Clean(p.Root), nil
	}
	anchor, value := anchors.FileDir, p.Rel
	if p.Repo != "" {
		anchor, value = anchors.Repo, p.Repo
	}
	if p.Home != "" {
		anchor, value = anchors.Home, p.Home
	}
	if !filepath.IsAbs(anchor) {
		return "", fmt.Errorf("selected anchor must be an explicit absolute directory")
	}
	return filepath.Join(anchor, filepath.FromSlash(value)), nil
}
