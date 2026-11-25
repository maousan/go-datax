package transform

import (
	"fmt"
	"strings"

	"github.com/longkeyy/go-datax/internal/connector"
)

// StepType is one supported transform operation.
type StepType string

const (
	StepProject StepType = "project"
	StepFilter  StepType = "filter"
	StepCast    StepType = "cast"
	StepRename  StepType = "rename"
	StepDrop    StepType = "drop"
)

// FunctionLibraryVersion is bumped when the fixed expression function library changes.
const FunctionLibraryVersion = 1

// ProjectColumn derives one output column from an expression.
type ProjectColumn struct {
	Name string             `json:"name"`
	Type connector.DataType `json:"type"`
	Expr string             `json:"expr"`
}

// CastColumn converts an existing column to another canonical type.
type CastColumn struct {
	Column string             `json:"column"`
	Type   connector.DataType `json:"type"`
}

// Rename renames an existing column.
type Rename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Step is one entry in the ordered transform chain. Only the fields belonging
// to Type are used.
type Step struct {
	Type      StepType        `json:"type"`
	Project   []ProjectColumn `json:"project,omitempty"`
	Condition string          `json:"condition,omitempty"`
	Cast      []CastColumn    `json:"cast,omitempty"`
	Rename    []Rename        `json:"rename,omitempty"`
	Drop      []string        `json:"drop,omitempty"`
}

// StepPreview is the schema before and after one step.
type StepPreview struct {
	Index        int              `json:"index"`
	Type         StepType         `json:"type"`
	InputSchema  connector.Schema `json:"inputSchema"`
	OutputSchema connector.Schema `json:"outputSchema"`
}

// Preview is the full derived transform chain.
type Preview struct {
	Steps        []StepPreview    `json:"steps"`
	OutputSchema connector.Schema `json:"outputSchema"`
}

func normalizeStep(step Step) (Step, error) {
	step.Type = StepType(strings.ToLower(strings.TrimSpace(string(step.Type))))
	step.Condition = strings.TrimSpace(step.Condition)
	for index := range step.Project {
		step.Project[index].Name = strings.TrimSpace(step.Project[index].Name)
		step.Project[index].Expr = strings.TrimSpace(step.Project[index].Expr)
	}
	for index := range step.Cast {
		step.Cast[index].Column = strings.TrimSpace(step.Cast[index].Column)
	}
	for index := range step.Rename {
		step.Rename[index].From = strings.TrimSpace(step.Rename[index].From)
		step.Rename[index].To = strings.TrimSpace(step.Rename[index].To)
	}
	for index := range step.Drop {
		step.Drop[index] = strings.TrimSpace(step.Drop[index])
	}
	switch step.Type {
	case StepProject:
		if len(step.Project) == 0 {
			return step, fmt.Errorf("project step requires at least one column")
		}
	case StepFilter:
		if step.Condition == "" {
			return step, fmt.Errorf("filter step requires a condition")
		}
	case StepCast:
		if len(step.Cast) == 0 {
			return step, fmt.Errorf("cast step requires at least one column")
		}
	case StepRename:
		if len(step.Rename) == 0 {
			return step, fmt.Errorf("rename step requires at least one column")
		}
	case StepDrop:
		if len(step.Drop) == 0 {
			return step, fmt.Errorf("drop step requires at least one column")
		}
	default:
		return step, fmt.Errorf("unsupported transform type %q", step.Type)
	}
	return step, nil
}

func uniqueName(name string, seen map[string]bool) error {
	key := strings.ToLower(name)
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("column name must not be empty")
	}
	if seen[key] {
		return fmt.Errorf("duplicate column %q", name)
	}
	seen[key] = true
	return nil
}
