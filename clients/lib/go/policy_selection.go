package directions

import "fmt"

// selectPolicies determines eligibility, not trigger applicability. By default
// every policy is eligible; the caller still applies diff/context triggers.
func selectPolicies(policies map[string]map[string]Policy, profile Profile) (map[string]Policy, error) {
	if profile.Whitelist != nil && profile.Blacklist != nil {
		return nil, fmt.Errorf("a profile cannot declare both whitelist and blacklist")
	}
	selected := map[string]Policy{}
	if profile.Whitelist == nil {
		for group, entries := range policies {
			for name, policy := range entries {
				selected[group+"."+name] = policy
			}
		}
	}
	list := profile.Whitelist
	if profile.Blacklist != nil {
		list = profile.Blacklist
	}
	if list == nil {
		return selected, nil
	}
	for _, ref := range *list {
		group, exists := policies[ref.Group]
		if !exists {
			return nil, fmt.Errorf("unknown policy group %q", ref.Group)
		}
		apply := func(name string, policy Policy) {
			key := ref.Group + "." + name
			if profile.Blacklist != nil {
				delete(selected, key)
			} else {
				selected[key] = policy
			}
		}
		if ref.Policy != "" {
			policy, exists := group[ref.Policy]
			if !exists {
				return nil, fmt.Errorf("unknown policy %s.%s", ref.Group, ref.Policy)
			}
			apply(ref.Policy, policy)
		} else {
			for name, policy := range group {
				apply(name, policy)
			}
		}
	}
	return selected, nil
}
