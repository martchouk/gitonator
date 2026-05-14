package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkflowRegistry holds all loaded workflow definitions indexed by their key.
type WorkflowRegistry struct {
	workflows  map[string]*WorkflowDef
	defaultKey string
}

// LoadWorkflowRegistry reads every *.yaml file in dir, parses those that contain
// a workflow.key field, validates them, and returns a ready registry.
// Returns an error if the defaultKey is not present after loading, or if any
// YAML file with a key fails validation.
func LoadWorkflowRegistry(dir, defaultKey string) (*WorkflowRegistry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read workflow dir %q: %w", dir, err)
	}

	reg := &WorkflowRegistry{
		workflows:  make(map[string]*WorkflowDef),
		defaultKey: defaultKey,
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", path, err)
		}

		// Pre-check: only fully parse files that declare a workflow.key.
		// Files without a key (e.g., the legacy extraction) may use YAML syntax
		// incompatible with the WorkflowDef struct and must be skipped silently.
		var header struct {
			Workflow struct {
				Key string `yaml:"key"`
			} `yaml:"workflow"`
		}
		if err := yaml.Unmarshal(data, &header); err != nil || header.Workflow.Key == "" {
			continue
		}

		var wd WorkflowDef
		if err := yaml.Unmarshal(data, &wd); err != nil {
			return nil, fmt.Errorf("parse %q: %w", path, err)
		}

		if err := validateWorkflowDef(&wd, path); err != nil {
			return nil, err
		}

		reg.workflows[wd.Workflow.Key] = &wd
	}

	if _, ok := reg.workflows[defaultKey]; !ok {
		return nil, fmt.Errorf("default workflow key %q not found in %q (loaded keys: %s)",
			defaultKey, dir, strings.Join(reg.Keys(), ", "))
	}

	return reg, nil
}

// Get returns the WorkflowDef for the given key. If the key is empty or unknown,
// it returns the default workflow definition.
func (r *WorkflowRegistry) Get(key string) *WorkflowDef {
	if wd, ok := r.workflows[strings.TrimSpace(key)]; ok {
		return wd
	}
	return r.workflows[r.defaultKey]
}

// Keys returns all loaded workflow keys.
func (r *WorkflowRegistry) Keys() []string {
	keys := make([]string, 0, len(r.workflows))
	for k := range r.workflows {
		keys = append(keys, k)
	}
	return keys
}

// validateWorkflowDef checks that all transition from/to values reference known
// status IDs, that every guard reference resolves, and that no non-terminal status
// is a dead end (has at least one outgoing transition).
func validateWorkflowDef(wd *WorkflowDef, path string) error {
	statusSet := make(map[string]bool, len(wd.Statuses))
	for _, s := range wd.Statuses {
		if s.ID == "" {
			return fmt.Errorf("%s: status entry has empty id", path)
		}
		statusSet[s.ID] = true
	}

	hasOutgoing := make(map[string]bool, len(wd.Statuses))

	for _, t := range wd.Transitions {
		if t.ID == "" {
			return fmt.Errorf("%s: transition entry has empty id", path)
		}

		// Validate to-status (dynamic targets are validated at runtime).
		if !strings.HasPrefix(t.To, "$") {
			if !statusSet[t.To] {
				return fmt.Errorf("%s: transition %q has unknown to_status %q", path, t.ID, t.To)
			}
		}

		// Validate from-statuses; nil means bootstrap transition — skip.
		for _, f := range t.From {
			if !statusSet[f] {
				return fmt.Errorf("%s: transition %q has unknown from_status %q", path, t.ID, f)
			}
			hasOutgoing[f] = true
		}

		// Validate guard reference.
		if t.Guard != "" {
			if _, ok := wd.Guards[t.Guard]; !ok {
				return fmt.Errorf("%s: transition %q references unknown guard %q", path, t.ID, t.Guard)
			}
		}
	}

	// Every non-terminal status must have at least one outgoing transition.
	for _, s := range wd.Statuses {
		if s.Terminal {
			continue
		}
		if !hasOutgoing[s.ID] {
			return fmt.Errorf("%s: non-terminal status %q has no outgoing transitions (dead end)", path, s.ID)
		}
	}

	return nil
}
