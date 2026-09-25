package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cgund98/gogent"
	"github.com/google/uuid"

	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

const planIgnoreLine = ".gopi/plans"

type writePlanArgs struct {
	PlanName string `json:"plan_name" jsonschema:"description=Short name for a new plan file. Used when path is omitted or the file does not exist."`
	Body     string `json:"body" jsonschema:"description=Full markdown plan."`
	Path     string `json:"path,omitempty" jsonschema:"description=Existing plan under .gopi/plans to overwrite. Omit to create a new file."`
}

// WritePlan creates or updates a markdown plan under <workspace>/.gopi/plans.
type WritePlan struct {
	Root      workspace.Root
	Workspace trust.Workspace
}

func (t *WritePlan) Name() string { return "write_plan" }

func (t *WritePlan) Description() string {
	return "Create or update a plan under <workspace>/.gopi/plans. Pass path to overwrite an existing plan file. Omit path, or pass a path that does not exist, to create <plan_name>-<uuid>.md. A plan inside the workspace does not ask for approval. An untrusted workspace is refused. Saving a plan adds .gopi/plans to the workspace-root .gitignore when that file already exists."
}

func (t *WritePlan) Parameters() json.RawMessage { return schemaFor(new(writePlanArgs)) }

func (t *WritePlan) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}

func (t *WritePlan) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	args, err := parseWritePlan(raw)
	if err != nil {
		return nil, err
	}
	if t.Workspace != trust.WorkspaceTrusted {
		return accessDenied(args.Path, "workspace is not trusted; edits are refused"), nil
	}
	rel, created, err := t.destination(args, true)
	if err != nil {
		target := args.Path
		if target == "" {
			target = args.PlanName
		}
		return accessDenied(target, err.Error()), nil
	}
	resolved := filepath.Join(t.Root.Path, rel)
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(args.Body), 0o644); err != nil {
		return nil, fmt.Errorf("write plan: %w", err)
	}
	ignoreUpdated, err := appendPlanIgnore(t.Root.Path)
	if err != nil {
		return nil, err
	}
	status := "updated"
	if created {
		status = "created"
	}
	return json.Marshal(map[string]any{
		"path":              rel,
		"status":            status,
		"gitignore_updated": ignoreUpdated,
	})
}

func parseWritePlan(raw json.RawMessage) (writePlanArgs, error) {
	var args writePlanArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, fmt.Errorf("parse arguments: %w", err)
	}
	if strings.TrimSpace(args.Body) == "" {
		return args, fmt.Errorf("body is required")
	}
	return args, nil
}

func (t *WritePlan) destination(args writePlanArgs, assignID bool) (rel string, created bool, err error) {
	if strings.TrimSpace(args.Path) != "" {
		resolved, outside, err := t.Root.Canonical(args.Path)
		if err != nil {
			return "", false, err
		}
		plans := filepath.Join(t.Root.Path, ".gopi", "plans")
		if outside || !insideDir(plans, resolved) || !strings.HasSuffix(resolved, ".md") {
			return "", false, fmt.Errorf("path must be an existing markdown file under .gopi/plans")
		}
		if _, statErr := os.Stat(resolved); statErr == nil {
			rel, err = filepath.Rel(t.Root.Path, resolved)
			return filepath.ToSlash(rel), false, err
		} else if !os.IsNotExist(statErr) {
			return "", false, statErr
		}
	}
	slug, err := planSlug(args.PlanName)
	if err != nil {
		return "", false, err
	}
	name := slug + "-<new>.md"
	if assignID {
		name = slug + "-" + uuid.NewString() + ".md"
	}
	return filepath.ToSlash(filepath.Join(".gopi", "plans", name)), true, nil
}

func planSlug(name string) (string, error) {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "", fmt.Errorf("plan_name is required")
	}
	return slug, nil
}

func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

func appendPlanIgnore(root string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if hasIgnoreLine(string(body), planIgnoreLine) {
		return false, nil
	}
	next := string(body)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += planIgnoreLine + "\n"
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return false, fmt.Errorf("update .gitignore: %w", err)
	}
	return true, nil
}

func hasIgnoreLine(body, line string) bool {
	for _, raw := range strings.Split(body, "\n") {
		if strings.TrimSpace(raw) == line {
			return true
		}
	}
	return false
}

var _ gogent.Tool = (*WritePlan)(nil)
