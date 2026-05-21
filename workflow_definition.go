package main

// WorkflowDef is the top-level structure loaded from a workflow YAML file.
type WorkflowDef struct {
	Workflow       WorkflowMeta        `yaml:"workflow"`
	IssueTypes     []IssueTypeDef      `yaml:"issue_types"`
	Statuses       []StatusDef         `yaml:"statuses"`
	Guards         map[string]GuardDef `yaml:"guards"`
	Transitions    []TransitionDef     `yaml:"transitions"`
	CanonicalPaths map[string][]string `yaml:"canonical_paths"`
}

// WorkflowMeta holds identity fields from the YAML workflow block.
type WorkflowMeta struct {
	ID               string   `yaml:"id"`
	Key              string   `yaml:"key"`
	Purpose          string   `yaml:"purpose"`
	Roles            []string `yaml:"roles"`
	SupportedTypes   []string `yaml:"supported_issue_types"`
	DefaultPathScope string   `yaml:"default_path_scope"`
}

// IssueTypeDef describes a type:* label and its default route metadata.
type IssueTypeDef struct {
	ID                 string   `yaml:"id"`
	Name               string   `yaml:"name"`
	PODefinitionOutput string   `yaml:"po_definition_output"`
	DefaultPath        []string `yaml:"default_path"`
}

// StatusDef describes a single workflow status as declared in a YAML file.
type StatusDef struct {
	ID         string `yaml:"id"`
	Role       string `yaml:"role"`
	QueuesWork bool   `yaml:"queues_work"`
	Terminal   bool   `yaml:"terminal"`
	Category   string `yaml:"category"`
}

// GuardDef describes a label-based precondition on a transition.
// AnyLabel: at least one of the listed labels must be present on the issue.
// AllAbsent: every listed label must be absent from the issue.
type GuardDef struct {
	Description string   `yaml:"description" json:"description,omitempty"`
	AnyLabel    []string `yaml:"any_label" json:"any_label,omitempty"`
	AllAbsent   []string `yaml:"all_absent" json:"all_absent,omitempty"`
}

// TransitionDef describes a single workflow transition as declared in a YAML file.
// To may be "$metadata.<key>" to resolve the target dynamically from stored metadata.
// SetMetadata entries may use "$from" to capture the current status at transition time.
type TransitionDef struct {
	ID                      string            `yaml:"id"`
	From                    []string          `yaml:"from"`
	To                      string            `yaml:"to"`
	AllowedRoles            []string          `yaml:"allowed_roles"`
	Guard                   string            `yaml:"guard"`
	SetMetadata             map[string]string `yaml:"set_metadata"`
	ClearMetadata           []string          `yaml:"clear_metadata"`
	Description             string            `yaml:"description"`
	CloseIssue              bool              `yaml:"close_issue"`
	ReopenIssue             bool              `yaml:"reopen_issue"`
	TerminalAfterTransition bool              `yaml:"terminal_after_transition"`
	// QueuesNextRole controls next_assignee_roles in the work package:
	//   nil       — field absent from YAML; fall back to role of the target status
	//   &""       — explicit empty (YAML ""); skip: this transition queues no agent role
	//   &"role"   — use this role
	QueuesNextRole  *string     `yaml:"queues_next_role"`
	RequiredOutputs interface{} `yaml:"required_outputs"` // list or map depending on transition type
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

// ValidTransitionsFrom returns all statically-resolvable target status IDs for
// transitions that originate from fromStatus. Dynamic targets (e.g., "$metadata.*")
// are excluded because they cannot be resolved without runtime metadata.
func (wd *WorkflowDef) ValidTransitionsFrom(fromStatus string) []string {
	var targets []string
	seen := map[string]bool{}
	for _, t := range wd.Transitions {
		if t.From == nil || len(t.To) == 0 || t.To[0] == '$' {
			continue
		}
		for _, f := range t.From {
			if f == fromStatus && !seen[t.To] {
				targets = append(targets, t.To)
				seen[t.To] = true
				break
			}
		}
	}
	return targets
}

// AllStatusIDs returns all status IDs declared in the workflow.
func (wd *WorkflowDef) AllStatusIDs() []string {
	ids := make([]string, 0, len(wd.Statuses))
	for _, s := range wd.Statuses {
		ids = append(ids, s.ID)
	}
	return ids
}

// HasRole reports whether role is used by any status in this workflow.
func (wd *WorkflowDef) HasRole(role string) bool {
	for _, s := range wd.Statuses {
		if s.Role == role {
			return true
		}
	}
	return false
}

// NextRolesFrom returns the unique agent roles that should receive work after the
// current status completes. It excludes self-loops (To == fromStatus), transitions
// to exception or terminal target statuses, and uses QueuesNextRole when set on
// the transition — falling back to the target status's role otherwise.
func (wd *WorkflowDef) NextRolesFrom(fromStatus string) []string {
	seen := map[string]bool{}
	var roles []string
	for _, t := range wd.Transitions {
		if t.From == nil || len(t.To) == 0 || t.To[0] == '$' {
			continue
		}
		if t.To == fromStatus {
			continue // skip self-loops
		}
		for _, f := range t.From {
			if f == fromStatus {
				sd := wd.StatusByID(t.To)
				if sd != nil && (sd.Terminal || sd.Category == "exception") {
					break // skip terminal and exception target statuses
				}
				var role string
				if t.QueuesNextRole != nil {
					role = *t.QueuesNextRole // explicit value: "" means skip, non-empty means use it
				} else if sd != nil {
					role = sd.Role // absent: fall back to target status role
				}
				if role != "" && !seen[role] {
					seen[role] = true
					roles = append(roles, role)
				}
				break
			}
		}
	}
	return roles
}
