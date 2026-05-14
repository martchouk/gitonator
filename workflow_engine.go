package main

import (
	"fmt"
	"strings"
)

// computeWorkflowStateFromDef computes the WorkflowState for an issue using a YAML WorkflowDef.
// SuggestedRole is derived from the status's role field in the definition.
func computeWorkflowStateFromDef(wd *WorkflowDef, issue Issue, comments []IssueComment) WorkflowState {
	status := ""
	var types []string
	for _, l := range issue.Labels {
		if strings.HasPrefix(l.Name, "status:") {
			status = l.Name
		}
		if strings.HasPrefix(l.Name, "type:") {
			types = append(types, l.Name)
		}
	}

	var assignees []string
	for _, a := range issue.Assignees {
		assignees = append(assignees, a.Login)
	}

	suggestedRole := "unknown"
	if sd := wd.StatusByID(status); sd != nil {
		suggestedRole = sd.Role
	}

	return WorkflowState{
		StatusLabel:      status,
		TypeLabels:       types,
		CurrentAssignees: assignees,
		SuggestedRole:    suggestedRole,
	}
}

// validateTransitionFromDef validates a role-based transition using a YAML WorkflowDef.
// meta holds the current issue_metadata values needed to resolve dynamic targets.
func validateTransitionFromDef(wd *WorkflowDef, issue Issue, meta map[string]string, actorRole, toStatus string) TransitionValidationResult {
	state := computeWorkflowStateFromDef(wd, issue, nil)
	actorRole = strings.TrimSpace(actorRole)

	res := TransitionValidationResult{
		Allowed:    false,
		ActorRole:  actorRole,
		FromStatus: state.StatusLabel,
		ToStatus:   toStatus,
	}

	if actorRole == "" {
		res.Violations = append(res.Violations, "actor_role is required")
		return res
	}
	if !wd.HasStatus(toStatus) {
		res.Violations = append(res.Violations, "target status is not recognized")
		return res
	}

	matched := findMatchingTransitionDef(wd, state.StatusLabel, toStatus, meta)
	if matched == nil {
		res.Violations = append(res.Violations, fmt.Sprintf("no transition rule from %s to %s", state.StatusLabel, toStatus))
		return res
	}

	res.MatchedRuleDescription = matched.Description

	if !containsString(matched.AllowedRoles, actorRole) {
		res.Violations = append(res.Violations, fmt.Sprintf("role %q is not allowed to perform this transition", actorRole))
	}

	if matched.Guard != "" {
		gd, ok := wd.Guards[matched.Guard]
		if ok && !evaluateGuard(gd, issue) {
			res.Violations = append(res.Violations, fmt.Sprintf("guard %q is not satisfied", matched.Guard))
		}
	}

	res.Allowed = len(res.Violations) == 0
	return res
}

// findMatchingTransitionDef returns the first transition in wd whose From list contains
// fromStatus and whose To value (after dynamic resolution) equals toStatus.
// Returns nil when no match is found.
func findMatchingTransitionDef(wd *WorkflowDef, fromStatus, toStatus string, meta map[string]string) *TransitionDef {
	for i := range wd.Transitions {
		t := &wd.Transitions[i]
		if t.From == nil {
			continue // bootstrap-only transition; skip
		}
		if !containsString(t.From, fromStatus) {
			continue
		}
		resolved, err := resolveTransitionTarget(t.To, meta)
		if err != nil {
			continue // unresolvable target (e.g., missing metadata)
		}
		if resolved == toStatus {
			return t
		}
	}
	return nil
}

// resolveTransitionTarget resolves a transition target string.
// Static targets (not starting with "$") are returned as-is.
// "$metadata.<key>" is resolved from the meta map; returns an error if key is missing.
func resolveTransitionTarget(to string, meta map[string]string) (string, error) {
	const metaPrefix = "$metadata."
	if !strings.HasPrefix(to, "$") {
		return to, nil
	}
	if strings.HasPrefix(to, metaPrefix) {
		key := strings.TrimPrefix(to, metaPrefix)
		val, ok := meta[key]
		if !ok || val == "" {
			return "", fmt.Errorf("metadata key %q is not set", key)
		}
		return val, nil
	}
	return "", fmt.Errorf("unrecognized dynamic target %q", to)
}

// evaluateGuard returns true when the issue satisfies the guard conditions.
// An empty guard (no conditions) is always satisfied.
func evaluateGuard(gd GuardDef, issue Issue) bool {
	labels := labelsToStrings(issue.Labels)
	labelSet := make(map[string]bool, len(labels))
	for _, l := range labels {
		labelSet[l] = true
	}

	if len(gd.AnyLabel) > 0 {
		found := false
		for _, l := range gd.AnyLabel {
			if labelSet[l] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, l := range gd.AllAbsent {
		if labelSet[l] {
			return false
		}
	}

	return true
}

// decideNextActionFromDef derives the next WorkPackage using a YAML WorkflowDef.
// Returns (WorkPackage{}, false) when the current status is terminal or does not queue work.
func decideNextActionFromDef(wd *WorkflowDef, cfg Config, issue Issue, state WorkflowState, comments []IssueComment) (WorkPackage, bool) {
	sd := wd.StatusByID(state.StatusLabel)
	if sd == nil || !sd.QueuesWork {
		return WorkPackage{}, false
	}

	var lastCommentID int64
	if len(comments) > 0 {
		lastCommentID = comments[len(comments)-1].ID
	}

	return WorkPackage{
		Repo:          fmt.Sprintf("%s/%s", cfg.Owner, cfg.Repo),
		IssueID:       issue.Number,
		Role:          sd.Role,
		Assignee:      currentAssigneeOfIssue(issue),
		LastCommentID: lastCommentID,
		CurrentStatus: state.StatusLabel,
	}, true
}

// applyTransitionMetadata writes set_metadata values and clears clear_metadata keys
// for the given transition definition. fromStatus is the pre-transition status,
// used to resolve the "$from" special value in set_metadata.
func (s *Server) applyTransitionMetadata(issueID int, fromStatus string, td *TransitionDef) {
	for k, v := range td.SetMetadata {
		val := v
		if val == "$from" {
			val = fromStatus
		}
		if err := s.store.SetIssueMetadata(issueID, k, val); err != nil {
			s.logger.Printf("set_metadata failed: issue=%d key=%s err=%v", issueID, k, err)
		}
	}
	if len(td.ClearMetadata) > 0 {
		if err := s.store.ClearIssueMetadata(issueID, td.ClearMetadata); err != nil {
			s.logger.Printf("clear_metadata failed: issue=%d keys=%v err=%v", issueID, td.ClearMetadata, err)
		}
	}
}
