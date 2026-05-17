package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type workflowFile struct {
	Workflow struct {
		Key   string   `yaml:"key"`
		Roles []string `yaml:"roles"`
	} `yaml:"workflow"`
	Statuses []struct {
		ID string `yaml:"id"`
	} `yaml:"statuses"`
	IssueTypes []struct {
		ID   string `yaml:"id"`
		Name string `yaml:"name"`
	} `yaml:"issue_types"`
	Guards map[string]struct {
		AnyLabel  []string `yaml:"any_label"`
		AllAbsent []string `yaml:"all_absent"`
	} `yaml:"guards"`
}

type labelDef struct {
	Name        string
	Color       string
	Description string
}

type syncTarget struct {
	WorkflowPath string
	OutputPath   string
}

var defaultTargets = []syncTarget{
	{WorkflowPath: "workflows/workflow-lean-3-roles-issue.yaml", OutputPath: "deploy/init_repo_lean.sh"},
	{WorkflowPath: "workflows/workflow-full-6-roles-issue.yaml", OutputPath: "deploy/init_repo_full.sh"},
}

var labelColors = map[string]string{
	"area:ui":                      "0ea5e9",
	"area:ux":                      "06b6d4",
	"needs:architecture":           "14b8a6",
	"needs:ui-design":              "10b981",
	"priority:high":                "e11d48",
	"priority:low":                 "84cc16",
	"priority:medium":              "f97316",
	"risk:high":                    "b91c1c",
	"role:architect":               "14b8a6",
	"role:developer":               "6366f1",
	"role:po":                      "ec4899",
	"role:reviewer":                "8b5cf6",
	"role:tester":                  "f59e0b",
	"role:uidesigner":              "10b981",
	"status:architecture-review":   "0f766e",
	"status:blocked":               "a530c8",
	"status:code-review":           "7c3aed",
	"status:dev-planning":          "0052cc",
	"status:done":                  "4af150",
	"status:in-development":        "f97316",
	"status:new":                   "598a80",
	"status:plan-review":           "7c3aed",
	"status:po-acceptance":         "c5a0f5",
	"status:po-approval":           "c5a0f5",
	"status:ready-for-dev":         "0052cc",
	"status:ready-for-development": "0ea5e9",
	"status:rejected":              "d6275b",
	"status:solution-design":       "14b8a6",
	"status:story-definition":      "0075ca",
	"status:testing":               "f59e0b",
	"status:triage":                "21c3d0",
	"status:ui-design":             "10b981",
	"status:ui-review":             "059669",
	"type:bug":                     "b60205",
	"type:change-request":          "c5def5",
	"type:feature":                 "a8a56e",
}

var labelDescriptions = map[string]string{
	"area:ui":                      "Issue affects UI",
	"area:ux":                      "Issue affects UX",
	"needs:architecture":           "Issue needs architecture design or review",
	"needs:ui-design":              "Issue needs UI design",
	"priority:high":                "High priority",
	"priority:low":                 "Low priority",
	"priority:medium":              "Medium priority",
	"risk:high":                    "High-risk issue that needs architecture handling",
	"role:architect":               "Architect-owned workflow step",
	"role:developer":               "Developer-owned workflow step",
	"role:po":                      "PO-owned workflow step",
	"role:reviewer":                "Reviewer-owned workflow step",
	"role:tester":                  "Tester-owned workflow step",
	"role:uidesigner":              "UI designer-owned workflow step",
	"status:architecture-review":   "Architect reviews implementation or design-sensitive changes",
	"status:blocked":               "Issue is blocked and PO owns coordination",
	"status:code-review":           "Reviewer reviews the implementation",
	"status:dev-planning":          "Developer creates or revises the development plan",
	"status:done":                  "Issue is approved and closed",
	"status:in-development":        "Developer is implementing the approved plan",
	"status:new":                   "Issue has been created and needs PO classification",
	"status:plan-review":           "Reviewer reviews the developer plan",
	"status:po-acceptance":         "PO performs final acceptance",
	"status:po-approval":           "PO approves rollout and closure",
	"status:ready-for-dev":         "Issue is ready for development",
	"status:ready-for-development": "Reviewed plan is accepted and development can begin",
	"status:rejected":              "Issue is rejected or closed without implementation",
	"status:solution-design":       "Architect designs the solution",
	"status:story-definition":      "PO defines the story or requirement",
	"status:testing":               "Tester verifies the implementation",
	"status:triage":                "PO triages the issue",
	"status:ui-design":             "UI designer prepares UI or UX design",
	"status:ui-review":             "UI designer reviews implemented UI",
	"type:bug":                     "Bug report",
	"type:change-request":          "Change request",
	"type:feature":                 "Feature request",
}

func main() {
	check := flag.Bool("check", false, "verify generated init_repo scripts are in sync without writing")
	workflowPath := flag.String("workflow", "", "workflow YAML path; requires -out")
	outPath := flag.String("out", "", "output shell script path; requires -workflow")
	flag.Parse()

	targets := defaultTargets
	if *workflowPath != "" || *outPath != "" {
		if *workflowPath == "" || *outPath == "" {
			fmt.Fprintln(os.Stderr, "-workflow and -out must be provided together")
			os.Exit(2)
		}
		targets = []syncTarget{{WorkflowPath: *workflowPath, OutputPath: *outPath}}
	}

	var failed bool
	for _, target := range targets {
		if err := syncOne(target, *check); err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func syncOne(target syncTarget, check bool) error {
	generated, err := generateInitRepoScript(target.WorkflowPath)
	if err != nil {
		return err
	}
	if check {
		current, err := os.ReadFile(target.OutputPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, generated) {
			return fmt.Errorf("%s is out of sync; run: go run ./deploy/sync_init_repo_scripts.go", target.OutputPath)
		}
		fmt.Printf("ok %s\n", target.OutputPath)
		return nil
	}
	if err := os.WriteFile(target.OutputPath, generated, 0o755); err != nil {
		return err
	}
	fmt.Printf("updated %s\n", target.OutputPath)
	return nil
}

func generateInitRepoScript(workflowPath string) ([]byte, error) {
	workflow, err := loadWorkflow(workflowPath)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(workflow.Workflow.Key)
	if key == "" {
		return nil, fmt.Errorf("%s: workflow.key is required", workflowPath)
	}
	labels := labelsFromWorkflow(workflow)

	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("# Generated by deploy/sync_init_repo_scripts.go; do not edit by hand.\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString("repo=\"${1:-${GH_REPO:-}}\"\n")
	b.WriteString("if [[ -z \"$repo\" ]]; then\n")
	b.WriteString("  repo=\"$(gh repo view --json nameWithOwner --jq .nameWithOwner)\"\n")
	b.WriteString("fi\n\n")
	b.WriteString("upsert_label() {\n")
	b.WriteString("  local name=\"$1\"\n")
	b.WriteString("  local color=\"$2\"\n")
	b.WriteString("  local description=\"$3\"\n\n")
	b.WriteString("  if gh label create \"$name\" --repo \"$repo\" --color \"$color\" --description \"$description\" >/dev/null 2>&1; then\n")
	b.WriteString("    printf 'created %s\\n' \"$name\"\n")
	b.WriteString("  else\n")
	b.WriteString("    gh label edit \"$name\" --repo \"$repo\" --color \"$color\" --description \"$description\" >/dev/null\n")
	b.WriteString("    printf 'updated %s\\n' \"$name\"\n")
	b.WriteString("  fi\n")
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "printf 'Initializing %s workflow labels in %%s\\n' \"$repo\"\n\n", shellLiteral(key))
	writeLabelGroup(&b, labels.statuses)
	writeLabelGroup(&b, labels.types)
	writeLabelGroup(&b, labels.priorities)
	writeLabelGroup(&b, labels.roles)
	writeLabelGroup(&b, labels.guards)
	fmt.Fprintf(&b, "printf '%s workflow labels initialized in %%s\\n' \"$repo\"\n", title(key))
	return []byte(b.String()), nil
}

func loadWorkflow(path string) (workflowFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return workflowFile{}, err
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return workflowFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return workflow, nil
}

type labelGroups struct {
	statuses   []labelDef
	types      []labelDef
	priorities []labelDef
	roles      []labelDef
	guards     []labelDef
}

func labelsFromWorkflow(workflow workflowFile) labelGroups {
	seen := map[string]bool{}
	var groups labelGroups

	for _, status := range workflow.Statuses {
		groups.statuses = appendIfNew(groups.statuses, seen, labelFor(status.ID))
	}
	for _, issueType := range workflow.IssueTypes {
		groups.types = appendIfNew(groups.types, seen, labelFor(issueType.ID))
	}
	for _, name := range []string{"priority:high", "priority:medium", "priority:low"} {
		groups.priorities = appendIfNew(groups.priorities, seen, labelFor(name))
	}
	for _, role := range workflow.Workflow.Roles {
		groups.roles = appendIfNew(groups.roles, seen, labelFor("role:"+role))
	}

	var guardLabels []string
	for _, guard := range workflow.Guards {
		guardLabels = append(guardLabels, guard.AnyLabel...)
		guardLabels = append(guardLabels, guard.AllAbsent...)
	}
	sort.Strings(guardLabels)
	for _, name := range guardLabels {
		if !strings.Contains(name, ":") || strings.HasPrefix(name, "status:") || strings.HasPrefix(name, "type:") {
			continue
		}
		def := labelFor(name)
		if strings.HasPrefix(name, "risk:") {
			groups.priorities = appendIfNew(groups.priorities, seen, def)
			continue
		}
		groups.guards = appendIfNew(groups.guards, seen, def)
	}

	return groups
}

func appendIfNew(labels []labelDef, seen map[string]bool, def labelDef) []labelDef {
	if def.Name == "" || seen[def.Name] {
		return labels
	}
	seen[def.Name] = true
	return append(labels, def)
}

func labelFor(name string) labelDef {
	return labelDef{
		Name:        name,
		Color:       lookup(labelColors, name, colorForPrefix(name)),
		Description: lookup(labelDescriptions, name, descriptionFromName(name)),
	}
}

func writeLabelGroup(b *strings.Builder, labels []labelDef) {
	if len(labels) == 0 {
		return
	}
	for _, label := range labels {
		fmt.Fprintf(b, "upsert_label %q %q %q\n", label.Name, label.Color, label.Description)
	}
	b.WriteString("\n")
}

func lookup(values map[string]string, key, fallback string) string {
	if v := values[key]; v != "" {
		return v
	}
	return fallback
}

func colorForPrefix(name string) string {
	switch {
	case strings.HasPrefix(name, "status:"):
		return "ededed"
	case strings.HasPrefix(name, "type:"):
		return "c5def5"
	case strings.HasPrefix(name, "role:"):
		return "6366f1"
	case strings.HasPrefix(name, "priority:"):
		return "f97316"
	case strings.HasPrefix(name, "risk:"):
		return "b91c1c"
	case strings.HasPrefix(name, "area:"), strings.HasPrefix(name, "needs:"):
		return "0ea5e9"
	default:
		return "ededed"
	}
}

func descriptionFromName(name string) string {
	parts := strings.SplitN(name, ":", 2)
	if len(parts) != 2 {
		return title(strings.ReplaceAll(name, "-", " "))
	}
	return title(parts[0]) + ": " + strings.ReplaceAll(parts[1], "-", " ")
}

func shellLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

func title(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
