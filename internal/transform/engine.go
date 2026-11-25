package transform

import (
	"fmt"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/longkeyy/go-datax/internal/connector"
)

// StepError identifies the failing transform step and row (both zero-based here).
type StepError struct {
	Step int
	Type StepType
	Row  int
	Err  error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %d (%s) row %d: %v", e.Step+1, e.Type, e.Row+1, e.Err)
}

func (e *StepError) Unwrap() error { return e.Err }

type preparedProject struct {
	column  ProjectColumn
	program *vm.Program
}

type preparedCast struct {
	index  int
	column CastColumn
}

type preparedStep struct {
	typ     StepType
	input   connector.Schema
	output  connector.Schema
	filter  *vm.Program
	project []preparedProject
	cast    []preparedCast
	rename  []Rename
	keep    []int
	env     map[string]any
	rowEnv  map[string]any
}

// Pipeline is a compiled, reusable transform chain. It is not safe for
// concurrent use because expression environments are reused across rows.
type Pipeline struct {
	input  connector.Schema
	output connector.Schema
	steps  []preparedStep
}

func (p *Pipeline) OutputSchema() connector.Schema { return p.output }

func (p *Pipeline) StepCount() int { return len(p.steps) }

// Prepare validates and compiles an ordered transform chain.
func Prepare(input connector.Schema, steps []Step) (*Pipeline, Preview, error) {
	if err := input.Validate(); err != nil {
		return nil, Preview{}, fmt.Errorf("input schema: %w", err)
	}
	pipeline := &Pipeline{input: input}
	preview := Preview{Steps: make([]StepPreview, 0, len(steps))}
	current := append(connector.Schema(nil), input...)
	for index, raw := range steps {
		step, err := normalizeStep(raw)
		if err != nil {
			return nil, Preview{}, fmt.Errorf("step %d: %w", index+1, err)
		}
		prepared, err := prepareStep(index, step, current)
		if err != nil {
			return nil, Preview{}, err
		}
		pipeline.steps = append(pipeline.steps, prepared)
		preview.Steps = append(preview.Steps, StepPreview{
			Index:        index,
			Type:         step.Type,
			InputSchema:  append(connector.Schema(nil), current...),
			OutputSchema: append(connector.Schema(nil), prepared.output...),
		})
		current = prepared.output
	}
	pipeline.output = current
	preview.OutputSchema = append(connector.Schema(nil), current...)
	return pipeline, preview, nil
}

// Validate compiles the chain and returns its preview.
func Validate(input connector.Schema, steps []Step) (Preview, error) {
	_, preview, err := Prepare(input, steps)
	return preview, err
}

func prepareStep(index int, step Step, input connector.Schema) (preparedStep, error) {
	prepared := preparedStep{
		typ:    step.Type,
		input:  append(connector.Schema(nil), input...),
		env:    schemaEnv(input),
		rowEnv: map[string]any{},
	}
	switch step.Type {
	case StepProject:
		seen := map[string]bool{}
		output := make(connector.Schema, 0, len(step.Project))
		for _, column := range step.Project {
			if !validDataType(column.Type) {
				return prepared, fmt.Errorf("step %d (project): unsupported type %q", index+1, column.Type)
			}
			if err := uniqueName(column.Name, seen); err != nil {
				return prepared, fmt.Errorf("step %d (project): %w", index+1, err)
			}
			if column.Expr == "" {
				return prepared, fmt.Errorf("step %d (project): column %q requires an expression", index+1, column.Name)
			}
			program, err := compile(column.Expr, input, false)
			if err != nil {
				return prepared, fmt.Errorf("step %d (project) column %q: %w", index+1, column.Name, err)
			}
			prepared.project = append(prepared.project, preparedProject{column: column, program: program})
			output = append(output, connector.Column{Name: column.Name, Type: column.Type})
		}
		prepared.output = output
	case StepFilter:
		program, err := compile(step.Condition, input, true)
		if err != nil {
			return prepared, fmt.Errorf("step %d (filter): %w", index+1, err)
		}
		prepared.filter = program
		prepared.output = append(connector.Schema(nil), input...)
	case StepCast:
		seen := map[string]bool{}
		output := append(connector.Schema(nil), input...)
		for _, cast := range step.Cast {
			if !validDataType(cast.Type) {
				return prepared, fmt.Errorf("step %d (cast): unsupported type %q", index+1, cast.Type)
			}
			columnIndex := input.Index(cast.Column)
			if columnIndex < 0 {
				return prepared, fmt.Errorf("step %d (cast): column %q does not exist", index+1, cast.Column)
			}
			if seen[strings.ToLower(cast.Column)] {
				return prepared, fmt.Errorf("step %d (cast): duplicate column %q", index+1, cast.Column)
			}
			seen[strings.ToLower(cast.Column)] = true
			output[columnIndex].Type = cast.Type
			prepared.cast = append(prepared.cast, preparedCast{index: columnIndex, column: cast})
		}
		prepared.output = output
	case StepRename:
		output := append(connector.Schema(nil), input...)
		seen := map[string]bool{}
		renamed := map[string]bool{}
		for _, rename := range step.Rename {
			if input.Index(rename.From) < 0 {
				return prepared, fmt.Errorf("step %d (rename): column %q does not exist", index+1, rename.From)
			}
			if rename.To == "" {
				return prepared, fmt.Errorf("step %d (rename): target name must not be empty", index+1)
			}
			key := strings.ToLower(rename.From)
			if renamed[key] {
				return prepared, fmt.Errorf("step %d (rename): duplicate source column %q", index+1, rename.From)
			}
			renamed[key] = true
			prepared.rename = append(prepared.rename, rename)
		}
		for _, rename := range prepared.rename {
			output[input.Index(rename.From)].Name = rename.To
		}
		for _, column := range output {
			if err := uniqueName(column.Name, seen); err != nil {
				return prepared, fmt.Errorf("step %d (rename): %w", index+1, err)
			}
		}
		prepared.output = output
	case StepDrop:
		drop := map[int]bool{}
		seen := map[string]bool{}
		for _, name := range step.Drop {
			columnIndex := input.Index(name)
			if columnIndex < 0 {
				return prepared, fmt.Errorf("step %d (drop): column %q does not exist", index+1, name)
			}
			if seen[strings.ToLower(name)] {
				return prepared, fmt.Errorf("step %d (drop): duplicate column %q", index+1, name)
			}
			seen[strings.ToLower(name)] = true
			drop[columnIndex] = true
		}
		output := make(connector.Schema, 0, len(input)-len(drop))
		for columnIndex, column := range input {
			if drop[columnIndex] {
				continue
			}
			output = append(output, column)
			prepared.keep = append(prepared.keep, columnIndex)
		}
		if len(output) == 0 {
			return prepared, fmt.Errorf("step %d (drop): cannot drop every column", index+1)
		}
		prepared.output = output
	default:
		return prepared, fmt.Errorf("step %d: unsupported transform type %q", index+1, step.Type)
	}
	if err := prepared.output.Validate(); err != nil {
		return prepared, fmt.Errorf("step %d output schema: %w", index+1, err)
	}
	prepared.env["_row"] = prepared.rowEnv
	return prepared, nil
}

func compile(code string, input connector.Schema, boolean bool) (*vm.Program, error) {
	env := schemaEnv(input)
	env["_row"] = map[string]any{}
	options := []expr.Option{expr.Env(env), expr.AsAny()}
	if boolean {
		options = []expr.Option{expr.Env(env), expr.AsBool()}
	}
	options = append(options, libraryOptions()...)
	return expr.Compile(code, options...)
}

func schemaEnv(schema connector.Schema) map[string]any {
	env := make(map[string]any, len(schema)+1)
	for _, column := range schema {
		env[column.Name] = zeroValue(column.Type)
	}
	return env
}

func zeroValue(dataType connector.DataType) any {
	switch dataType {
	case connector.TypeInt64:
		return int64(0)
	case connector.TypeFloat64:
		return float64(0)
	case connector.TypeBool:
		return false
	case connector.TypeBytes:
		return []byte{}
	case connector.TypeTimestamp:
		return time.Time{}
	default:
		return ""
	}
}

func validDataType(dataType connector.DataType) bool {
	schema := connector.Schema{{Name: "x", Type: dataType}}
	return schema.Validate() == nil
}

// Transform applies the chain to one batch. Filtered-out rows disappear.
func (p *Pipeline) Transform(batch connector.Batch) (connector.Batch, error) {
	if !connector.SchemasEqual(batch.Schema, p.input) {
		return connector.Batch{}, fmt.Errorf("batch schema does not match transform input")
	}
	rows := make([][]any, 0, len(batch.Rows))
	for rowIndex, row := range batch.Rows {
		current := row
		keep := true
		for stepIndex := range p.steps {
			step := &p.steps[stepIndex]
			step.bind(current)
			next, ok, err := step.apply(current)
			if err != nil {
				return connector.Batch{}, &StepError{
					Step: stepIndex,
					Type: step.typ,
					Row:  rowIndex,
					Err:  err,
				}
			}
			if !ok {
				keep = false
				break
			}
			current = next
		}
		if keep {
			rows = append(rows, current)
		}
	}
	return connector.Batch{Schema: p.output, Rows: rows}, nil
}

func (s *preparedStep) bind(row []any) {
	clear(s.env)
	clear(s.rowEnv)
	for index, column := range s.input {
		value := row[index]
		s.env[column.Name] = value
		s.rowEnv[column.Name] = value
	}
	s.env["_row"] = s.rowEnv
}

func (s *preparedStep) apply(row []any) ([]any, bool, error) {
	switch s.typ {
	case StepProject:
		next := make([]any, len(s.project))
		for index, project := range s.project {
			value, err := expr.Run(project.program, s.env)
			if err != nil {
				return nil, false, fmt.Errorf("column %s: %w", project.column.Name, err)
			}
			converted, err := connector.CoerceValue(value, project.column.Type)
			if err != nil {
				return nil, false, fmt.Errorf("column %s: %w", project.column.Name, err)
			}
			next[index] = converted
		}
		return next, true, nil
	case StepFilter:
		value, err := expr.Run(s.filter, s.env)
		if err != nil {
			return nil, false, err
		}
		matched, ok := value.(bool)
		if !ok {
			return nil, false, fmt.Errorf("condition returned %T, not bool", value)
		}
		return row, matched, nil
	case StepCast:
		next := append([]any(nil), row...)
		for _, cast := range s.cast {
			converted, err := connector.CoerceValue(next[cast.index], cast.column.Type)
			if err != nil {
				return nil, false, fmt.Errorf("column %s: %w", cast.column.Column, err)
			}
			next[cast.index] = converted
		}
		return next, true, nil
	case StepRename:
		return row, true, nil
	case StepDrop:
		next := make([]any, len(s.keep))
		for index, columnIndex := range s.keep {
			next[index] = row[columnIndex]
		}
		return next, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported transform type %q", s.typ)
	}
}
