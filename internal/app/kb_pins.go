package app

// pinnedKBNodes returns the knowledge bases shown in the sidebar's 常用
// list, in the order of the full tree. A nil pin list keeps every
// knowledge base there, so existing vaults do not lose their rows.
func (a *app) pinnedKBNodes() []*Node {
	if a.cfg == nil || a.cfg.PinnedKBs == nil {
		return a.tree
	}
	pinned := make(map[string]bool, len(*a.cfg.PinnedKBs))
	for _, rel := range *a.cfg.PinnedKBs {
		pinned[rel] = true
	}
	nodes := make([]*Node, 0, len(pinned))
	for _, n := range a.tree {
		if pinned[n.Rel] {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// isKBPinned reports whether rel is in the sidebar's 常用 list.
func (a *app) isKBPinned(rel string) bool {
	if a.cfg == nil || a.cfg.PinnedKBs == nil {
		return true
	}
	for _, name := range *a.cfg.PinnedKBs {
		if name == rel {
			return true
		}
	}
	return false
}

// setKBPinned adds or removes a knowledge base in the 常用 list and saves
// the choice. The list is materialized in tree order.
func (a *app) setKBPinned(rel string, pinned bool) error {
	if a.cfg == nil {
		return nil
	}
	want := make(map[string]bool, len(a.tree)+1)
	if a.cfg.PinnedKBs == nil {
		for _, n := range a.tree {
			want[n.Rel] = true
		}
	} else {
		for _, name := range *a.cfg.PinnedKBs {
			want[name] = true
		}
	}
	if pinned {
		want[rel] = true
	} else {
		delete(want, rel)
	}
	names := make([]string, 0, len(want))
	for _, n := range a.tree {
		if want[n.Rel] {
			names = append(names, n.Rel)
		}
	}
	a.cfg.PinnedKBs = &names
	return saveConfig(a.cfg)
}

// rememberNewKBPin pins a knowledge base as it is created, unless the vault
// still uses the old default of showing every knowledge base.
func (a *app) rememberNewKBPin(rel string) error {
	if a.cfg == nil || a.cfg.PinnedKBs == nil {
		return nil
	}
	for _, name := range *a.cfg.PinnedKBs {
		if name == rel {
			return nil
		}
	}
	names := append(append([]string(nil), (*a.cfg.PinnedKBs)...), rel)
	a.cfg.PinnedKBs = &names
	return saveConfig(a.cfg)
}

// renameKBPin follows a renamed knowledge base in the 常用 list.
func (a *app) renameKBPin(oldRel, newRel string) error {
	if a.cfg == nil || a.cfg.PinnedKBs == nil {
		return nil
	}
	names := append([]string(nil), (*a.cfg.PinnedKBs)...)
	changed := false
	for i, name := range names {
		if name == oldRel {
			names[i] = newRel
			changed = true
		}
	}
	if !changed {
		return nil
	}
	a.cfg.PinnedKBs = &names
	return saveConfig(a.cfg)
}

// forgetKBPin drops a deleted or removed knowledge base from the 常用 list.
func (a *app) forgetKBPin(rel string) error {
	if a.cfg == nil || a.cfg.PinnedKBs == nil {
		return nil
	}
	names := make([]string, 0, len(*a.cfg.PinnedKBs))
	for _, name := range *a.cfg.PinnedKBs {
		if name != rel {
			names = append(names, name)
		}
	}
	a.cfg.PinnedKBs = &names
	return saveConfig(a.cfg)
}

// createKnowledgeBase makes a knowledge base and keeps the 常用 list in
// step when the vault already has an explicit list.
func (a *app) createKnowledgeBase(name string) (string, error) {
	rel, err := a.vault.CreateKnowledgeBase(name)
	if err != nil {
		return "", err
	}
	if err := a.rememberNewKBPin(rel); err != nil {
		a.setError(err)
	}
	return rel, nil
}
