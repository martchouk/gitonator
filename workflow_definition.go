package main

// WorkflowDef is the top-level structure loaded from a workflow YAML file.
type WorkflowDef struct {
	Workflow    WorkflowMeta        `yaml:"workflow"`
	Statuses    []StatusDef         `yaml:"statuses"`
	Guards      map[string]GuardDef `yaml:"guards"`
	Transitions []TransitionDef     `yaml:"transitions"`
}

// WorkflowMeta holds identity fields from the YAML workflow block.
type WorkflowMeta struct {
	ID  string `yaml:"id"`
	Key string `yaml:"key"`
}

// StatusDef describes a single workflow status as declared in a YAML file.
type StatusDef struct {
	ID         string `yaml:"id"`
	Role       string `yaml:"role"`
	QueuesWork bool   `yaml:"queues_work"`
	Terminal   bool   `yaml:"terminal"`
}

// GuardDef describes a label-based precondition on a transition.
// AnyLabel: at least one of the listed labels must be present on the issue.
// AllAbsent: every listed label must be absent from the issue.
type GuardDef struct {
	AnyLabel  []string `yaml:"any_label"`
	AllAbsent []string `yaml:"all_absent"`
}

// TransitionDef describes a single workflow transition as declared in a YAML file.
// To may be "$metadata.<key>" to resolve the target dynamically from stored metadata.
// SetMetadata entries may use "$from" to capture the current status at transition time.
type TransitionDef struct {
	ID            string            `yaml:"id"`
	From          []string          `yaml:"from"`
	To            string            `yaml:"to"`
	AllowedRoles  []string          `yaml:"allowed_roles"`
	Guard         string            `yaml:"guard"`
	SetMetadata   map[string]string `yaml:"set_metadata"`
	ClearMetadata []string          `yaml:"clear_metadata"`
	Description   string            `yaml:"description"`
}

// StatusByID returns the StatusDef for the given status ID, or nil if not found.
func (wd *WorkflowDef) StatusByID(id string) *StatusDef {
	for i := range wd.Statuses {
		if wd.Statuses[i].ID == id {
			return &wd.Statuses[i]
		}
	}
	return nil
}

// HasStatus reports whether id is a known status in this workflow.
func (wd *WorkflowDef) HasStatus(id string) bool {
	return wd.StatusByID(id) != nil
}

// AllStatusIDs returns all status IDs declared in the workflow.
func (wd *WorkflowDef) AllStatusIDs() []string {
	ids := make([]string, 0, len(wd.Statuses))
	for _, s := range wd.Statuses {
		ids = append(ids, s.ID)
	}
	return ids
}
