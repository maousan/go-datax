package connector

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DataType is the canonical type shared by all connectors.
// There is no null type: every column has a type, and any value may be nil.
type DataType string

const (
	TypeInt64     DataType = "int64"
	TypeFloat64   DataType = "float64"
	TypeDecimal   DataType = "decimal"
	TypeString    DataType = "string"
	TypeBytes     DataType = "bytes"
	TypeBool      DataType = "bool"
	TypeTimestamp DataType = "timestamp"
)

var validDataTypes = map[DataType]bool{
	TypeInt64:     true,
	TypeFloat64:   true,
	TypeDecimal:   true,
	TypeString:    true,
	TypeBytes:     true,
	TypeBool:      true,
	TypeTimestamp: true,
}

// Column declares one output/input column.
type Column struct {
	Name string   `json:"name"`
	Type DataType `json:"type"`
}

// Schema is an ordered, explicit column list.
type Schema []Column

// Validate rejects empty, duplicate or untyped schemas.
func (s Schema) Validate() error {
	if len(s) == 0 {
		return fmt.Errorf("schema must not be empty")
	}
	seen := map[string]bool{}
	for _, column := range s {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			return fmt.Errorf("column name must not be empty")
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate column %q", name)
		}
		seen[key] = true
		if !validDataTypes[column.Type] {
			return fmt.Errorf("unsupported column type %q", column.Type)
		}
	}
	return nil
}

// Names returns the column names in schema order.
func (s Schema) Names() []string {
	names := make([]string, len(s))
	for index, column := range s {
		names[index] = column.Name
	}
	return names
}

// Index returns the position of a column name, or -1.
func (s Schema) Index(name string) int {
	for index, column := range s {
		if strings.EqualFold(column.Name, name) {
			return index
		}
	}
	return -1
}

// Batch is a schema-bound set of rows.
type Batch struct {
	Schema Schema
	Rows   [][]any
	Bytes  int64
}

// Validate checks the schema and every row width.
func (b Batch) Validate() error {
	if err := b.Schema.Validate(); err != nil {
		return err
	}
	for index, row := range b.Rows {
		if len(row) != len(b.Schema) {
			return &RowError{Row: index, Err: fmt.Errorf("expected %d columns, got %d", len(b.Schema), len(row))}
		}
	}
	return nil
}

// Split is one independently readable unit produced by a Source.
type Split struct {
	ID   string
	SQL  string
	Args []any
}

// Source reads canonical batches.
type Source interface {
	Schema() Schema
	Splits(ctx context.Context) ([]Split, error)
	ReadBatch(ctx context.Context, split Split) (Batch, error)
	Close() error
}

// Sink writes canonical batches.
type Sink interface {
	WriteBatch(ctx context.Context, batch Batch) error
	Close() error
}

// Config carries the resolved definition and its DataSource.
type Config struct {
	Connector            string
	DataSourceType       string
	JDBCUrl              string
	DataSourceProperties map[string]string
	Schema               Schema
	Options              map[string]string
}

// RowError identifies the failing row.
// Sources use a zero-based index within the split; sinks use a zero-based
// index within the batch.
type RowError struct {
	Row int
	Err error
}

func (e *RowError) Error() string {
	return fmt.Sprintf("row %d: %v", e.Row, e.Err)
}

func (e *RowError) Unwrap() error {
	return e.Err
}

func validateOptionKeys(options map[string]string, allowed ...string) error {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	for key := range options {
		if !known[key] {
			return fmt.Errorf("unsupported option %q", key)
		}
	}
	return nil
}

func option(options map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(options[key]); value != "" {
		return value
	}
	return fallback
}

func requiredOption(options map[string]string, key string) (string, error) {
	value := strings.TrimSpace(options[key])
	if value == "" {
		return "", fmt.Errorf("option %q is required", key)
	}
	return value, nil
}

func boolOption(options map[string]string, key string, fallback bool) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(options[key]))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("option %q must be true or false", key)
	}
}

func delimiterOption(options map[string]string, fallback rune) (rune, error) {
	value := options["delimiter"]
	if value == "" {
		return fallback, nil
	}
	var delimiter rune
	switch value {
	case `\t`:
		delimiter = '\t'
	case `\\`:
		delimiter = '\\'
	default:
		if strings.HasPrefix(value, `\u`) && len(value) == 6 {
			parsed, err := strconv.ParseUint(value[2:], 16, 32)
			if err != nil {
				return 0, fmt.Errorf("option \"delimiter\" must be a single character")
			}
			delimiter = rune(parsed)
		} else {
			runes := []rune(value)
			if len(runes) != 1 {
				return 0, fmt.Errorf("option \"delimiter\" must be a single character")
			}
			delimiter = runes[0]
		}
	}
	switch delimiter {
	case 0, '\n', '\r', utf8.RuneError:
		return 0, fmt.Errorf("option \"delimiter\" must not be NUL, a newline or an invalid rune")
	}
	return delimiter, nil
}
