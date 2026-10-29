// Package directions parses draft Directions documents and assembles bounded
// guidance. Parsing is pure; BuildContext reads local files and host-approved
// HTTPS sources, but never credentials or instructions as code.
package directions

import (
	"bytes"
	"fmt"
	"path"
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

type PolicyRef struct {
	Group  string `toml:"group"`
	Policy string `toml:"policy"`
}

type Profile struct {
	Prompt   string `toml:"prompt"` // project-root-relative path or HTTPS URL
	Model    string `toml:"model"`
	Thinking string `toml:"thinking"`
	// nil means absent; a pointer to an empty slice is an explicit empty list.
	Whitelist *[]PolicyRef `toml:"whitelist"`
	Blacklist *[]PolicyRef `toml:"blacklist"`
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
	Path string `toml:"path"` // project-root-relative file/glob or HTTPS URL
}

type Document struct {
	Meta      *Meta                        `toml:"meta"`
	Repo      *Repo                        `toml:"repo"`
	Profiles  map[string]Profile           `toml:"profiles"`
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

func parse(data []byte, fragment bool) (*Document, error) {
	var doc Document
	if err := decode(data, &doc); err != nil {
		return nil, fmt.Errorf("TOML: %w", err)
	}
	if !fragment && (doc.Meta == nil || doc.Repo == nil) {
		return nil, fmt.Errorf("base document requires meta.version and repo.vcs")
	}
	if doc.Meta != nil && doc.Meta.Version != Version {
		return nil, fmt.Errorf("unsupported meta.version %q", doc.Meta.Version)
	}
	if doc.Repo != nil && doc.Repo.VCS != "git" && doc.Repo.VCS != "none" {
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
	for name, profile := range doc.Profiles {
		if !validName(name) {
			return nil, fmt.Errorf("invalid profile name %q", name)
		}
		if profile.Whitelist != nil && profile.Blacklist != nil {
			return nil, fmt.Errorf("profiles.%s cannot declare both whitelist and blacklist", name)
		}
		if profile.Model != "" && !validModelRef(profile.Model) {
			return nil, fmt.Errorf("profiles.%s model must be a qualified provider/model reference", name)
		}
		if profile.Prompt != "" {
			if err := validateReference(profile.Prompt); err != nil {
				return nil, fmt.Errorf("profiles.%s.prompt: %w", name, err)
			}
		}
		for _, list := range []*[]PolicyRef{profile.Whitelist, profile.Blacklist} {
			if list == nil {
				continue
			}
			for _, ref := range *list {
				if !validName(ref.Group) || (ref.Policy != "" && !validName(ref.Policy)) {
					return nil, fmt.Errorf("profiles.%s has an invalid policy selector", name)
				}
			}
		}
	}
	for group, policies := range doc.Policies {
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
					if err := validateLocalPath(pattern); err != nil {
						return nil, fmt.Errorf("policies.%s.%s triggers.diff: %w", group, name, err)
					}
				}
			}
		}
	}
	for i, override := range doc.Overrides {
		if err := validateReference(override.Path); err != nil {
			return nil, fmt.Errorf("overrides[%d]: %w", i, err)
		}
	}
	return &doc, nil
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
	profiles, _ := keys["profiles"].(map[string]any)
	for _, value := range profiles {
		profile, _ := value.(map[string]any)
		if err := checkStrings(profile, "prompt", "model", "thinking"); err != nil {
			return err
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

// validateLocalPath checks portable project-relative syntax, not filesystem access.
func validateLocalPath(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
		return fmt.Errorf("path must be nonempty and contain no NUL")
	}
	if strings.ContainsAny(value, "\\:") || strings.HasPrefix(value, "~") || path.IsAbs(value) {
		return fmt.Errorf("paths must be project-root-relative with forward slashes; absolute paths, tilde expansion and URLs are unsupported")
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path escapes the project root")
	}
	return nil
}
