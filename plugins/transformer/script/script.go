// Package script implements the dx_script record-level transformer on top of
// the go-scripts embedded scripting engines.
//
// A script mutates the current record through a small, language-neutral host
// API (get_string/set_long/...). Returning false filters the record; any other
// return value keeps it. Supported languages: javascript, starlark, expr.
package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/longkeyy/go-datax/common/config"
	"github.com/longkeyy/go-datax/common/element"
	"github.com/longkeyy/go-datax/common/logger"
	"github.com/longkeyy/go-datax/common/transformer"
	"go.uber.org/zap"
)

func init() {
	transformer.RegisterRecord("dx_script", newScriptTransformer)
}

// scriptRuntime is one prepared embedded engine.
type scriptRuntime interface {
	// Prepare initialises the engine, registers host functions and compiles
	// the script. It is called once during transformer construction.
	Prepare() error
	// Transform executes the per-record entry point. It reports whether the
	// record should be kept; mutations are applied through recordAPI.
	Transform() (keep bool, err error)
	// Close releases the engine.
	Close() error
}

type runtimeFactory func(code string, api *recordAPI, timeout time.Duration) (scriptRuntime, error)

type languageSpec struct {
	factory         runtimeFactory
	supportsTimeout bool
}

var languages = make(map[string]languageSpec)

func registerLanguage(name string, spec languageSpec) {
	if _, exists := languages[name]; exists {
		panic("dx_script: duplicate language registration for " + name)
	}
	languages[name] = spec
}

func supportedLanguages() []string {
	names := make([]string, 0, len(languages))
	for name := range languages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type scriptTransformer struct {
	language string
	api      *recordAPI
	runtime  scriptRuntime
}

func newScriptTransformer(params config.Configuration) (transformer.Transformer, error) {
	language := strings.ToLower(strings.TrimSpace(params.GetString("language")))
	if language == "" {
		return nil, fmt.Errorf("dx_script: parameter.language is required (supported: %s)",
			strings.Join(supportedLanguages(), ", "))
	}

	spec, exists := languages[language]
	if !exists {
		return nil, fmt.Errorf("dx_script: unsupported language %q (supported: %s)",
			language, strings.Join(supportedLanguages(), ", "))
	}

	code, err := loadCode(params)
	if err != nil {
		return nil, err
	}

	timeoutMs := params.GetIntWithDefault("timeoutMs", 0)
	if timeoutMs < 0 {
		return nil, fmt.Errorf("dx_script: timeoutMs must be non-negative, got %d", timeoutMs)
	}
	if timeoutMs > 0 && !spec.supportsTimeout {
		return nil, fmt.Errorf("dx_script: timeoutMs is only supported for javascript")
	}

	api := newRecordAPI()
	runtime, err := spec.factory(code, api, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("dx_script (%s): %v", language, err)
	}
	if err := runtime.Prepare(); err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("dx_script (%s): %v", language, err)
	}

	return &scriptTransformer{
		language: language,
		api:      api,
		runtime:  runtime,
	}, nil
}

// loadCode resolves parameter.code or parameter.codeFile. Relative codeFile
// paths are resolved against $GO_DATAX_HOME (falling back to $ADDAX_HOME for
// easy migration).
func loadCode(params config.Configuration) (string, error) {
	code := params.GetString("code")
	codeFile := params.GetString("codeFile")

	switch {
	case code != "" && codeFile != "":
		return "", fmt.Errorf("dx_script: parameter.code and parameter.codeFile are mutually exclusive")
	case code != "":
		return code, nil
	case codeFile == "":
		return "", fmt.Errorf("dx_script: parameter.code or parameter.codeFile is required")
	}

	path := codeFile
	if !filepath.IsAbs(path) {
		home := os.Getenv("GO_DATAX_HOME")
		if home == "" {
			home = os.Getenv("ADDAX_HOME")
		}
		if home == "" {
			return "", fmt.Errorf("dx_script: relative codeFile %q requires GO_DATAX_HOME (or ADDAX_HOME) to be set", codeFile)
		}
		path = filepath.Join(home, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("dx_script: cannot read codeFile %q: %v", path, err)
	}
	return string(data), nil
}

func (t *scriptTransformer) Name() string { return "dx_script" }

func (t *scriptTransformer) Evaluate(record element.Record) (element.Record, error) {
	t.api.reset(record)

	keep, err := t.runtime.Transform()
	if err != nil {
		return nil, fmt.Errorf("dx_script (%s): %v", t.language, err)
	}
	if t.api.err != nil {
		return nil, fmt.Errorf("dx_script (%s): %v", t.language, t.api.err)
	}
	if !keep {
		return nil, nil
	}
	return record, nil
}

// Close releases the underlying scripting engine.
func (t *scriptTransformer) Close() error {
	return t.runtime.Close()
}

// ---------------------------------------------------------------------------
// record API exposed to scripts
// ---------------------------------------------------------------------------

// recordAPI is the language-neutral bridge between scripts and element.Record.
// It is used from a single task-group goroutine, so no locking is required.
type recordAPI struct {
	record element.Record
	err    error
}

func newRecordAPI() *recordAPI {
	return &recordAPI{}
}

func (a *recordAPI) reset(record element.Record) {
	a.record = record
	a.err = nil
}

// functions returns the host functions registered in every language.
func (a *recordAPI) functions() map[string]any {
	return map[string]any{
		"column_count": a.ColumnCount,
		"get_type":     a.GetType,
		"is_null":      a.IsNull,
		"get_string":   a.GetString,
		"get_long":     a.GetLong,
		"get_double":   a.GetDouble,
		"get_bool":     a.GetBool,
		"get_bytes":    a.GetBytes,
		"set_string":   a.SetString,
		"set_long":     a.SetLong,
		"set_double":   a.SetDouble,
		"set_bool":     a.SetBool,
		"set_date":     a.SetDate,
		"set_bytes":    a.SetBytes,
		"set_null":     a.SetNull,
		"log":          a.Log,
	}
}

func (a *recordAPI) fail(format string, args ...interface{}) {
	if a.err == nil {
		a.err = fmt.Errorf(format, args...)
	}
}

func (a *recordAPI) column(index int) element.Column {
	if a.err != nil {
		return nil
	}
	if a.record == nil || index < 0 || index >= a.record.GetColumnNumber() {
		a.fail("column index %d out of range", index)
		return nil
	}
	return a.record.GetColumn(index)
}

// ColumnCount returns the number of columns in the current record.
func (a *recordAPI) ColumnCount() int {
	if a.record == nil {
		return 0
	}
	return a.record.GetColumnNumber()
}

// GetType returns the column type name, e.g. "string" or "long".
func (a *recordAPI) GetType(index int) string {
	column := a.column(index)
	if a.err != nil {
		return ""
	}
	if column == nil {
		return "null"
	}
	return columnTypeName(column.GetType())
}

// IsNull reports whether the column is null.
func (a *recordAPI) IsNull(index int) bool {
	column := a.column(index)
	if a.err != nil {
		return true
	}
	return column == nil || column.IsNull()
}

// GetString returns the column as a string ("" for null).
func (a *recordAPI) GetString(index int) string {
	column := a.column(index)
	if a.err != nil || column == nil || column.IsNull() {
		return ""
	}
	return column.GetAsString()
}

// GetLong returns the column as an integer (dates use Unix milliseconds).
func (a *recordAPI) GetLong(index int) int64 {
	column := a.column(index)
	if a.err != nil || column == nil || column.IsNull() {
		return 0
	}
	value, err := column.GetAsLong()
	if err != nil {
		a.fail("column %d: cannot read as long: %v", index, err)
		return 0
	}
	return value
}

// GetDouble returns the column as a float.
func (a *recordAPI) GetDouble(index int) float64 {
	column := a.column(index)
	if a.err != nil || column == nil || column.IsNull() {
		return 0
	}
	value, err := column.GetAsDouble()
	if err != nil {
		a.fail("column %d: cannot read as double: %v", index, err)
		return 0
	}
	return value
}

// GetBool returns the column as a boolean.
func (a *recordAPI) GetBool(index int) bool {
	column := a.column(index)
	if a.err != nil || column == nil || column.IsNull() {
		return false
	}
	value, err := column.GetAsBool()
	if err != nil {
		a.fail("column %d: cannot read as bool: %v", index, err)
		return false
	}
	return value
}

// GetBytes returns the column as a string backed by its raw bytes.
func (a *recordAPI) GetBytes(index int) string {
	column := a.column(index)
	if a.err != nil || column == nil || column.IsNull() {
		return ""
	}
	value, err := column.GetAsBytes()
	if err != nil {
		a.fail("column %d: cannot read as bytes: %v", index, err)
		return ""
	}
	return string(value)
}

// SetString replaces the column with a string value.
func (a *recordAPI) SetString(index int, value string) bool {
	return a.set(index, element.NewStringColumn(value))
}

// SetLong replaces the column with an integer value.
func (a *recordAPI) SetLong(index int, value int64) bool {
	return a.set(index, element.NewLongColumn(value))
}

// SetDouble replaces the column with a float value.
func (a *recordAPI) SetDouble(index int, value float64) bool {
	return a.set(index, element.NewDoubleColumn(value))
}

// SetBool replaces the column with a boolean value.
func (a *recordAPI) SetBool(index int, value bool) bool {
	return a.set(index, element.NewBoolColumn(value))
}

// SetDate replaces the column with a date from Unix milliseconds.
func (a *recordAPI) SetDate(index int, millis int64) bool {
	return a.set(index, element.NewDateColumn(time.UnixMilli(millis)))
}

// SetBytes replaces the column with raw bytes.
func (a *recordAPI) SetBytes(index int, value string) bool {
	return a.set(index, element.NewBytesColumn([]byte(value)))
}

// SetNull replaces the column with a null value of the same type.
func (a *recordAPI) SetNull(index int) bool {
	column := a.column(index)
	if a.err != nil {
		return false
	}
	columnType := element.TypeString
	if column != nil {
		columnType = column.GetType()
	}
	return a.set(index, nullColumn(columnType))
}

func (a *recordAPI) set(index int, column element.Column) bool {
	if a.err != nil {
		return false
	}
	if a.record == nil || index < 0 || index >= a.record.GetColumnNumber() {
		a.fail("column index %d out of range", index)
		return false
	}
	a.record.SetColumn(index, column)
	return true
}

// Log writes a message to the task log at debug level. It returns true so it
// can be chained inside expression languages.
func (a *recordAPI) Log(message string) bool {
	logger.Component().WithComponent("ScriptTransformer").Debug("script log", zap.String("message", message))
	return true
}

func columnTypeName(columnType element.ColumnType) string {
	switch columnType {
	case element.TypeLong:
		return "long"
	case element.TypeDouble:
		return "double"
	case element.TypeString:
		return "string"
	case element.TypeDate:
		return "date"
	case element.TypeBool:
		return "bool"
	case element.TypeBytes:
		return "bytes"
	default:
		return "null"
	}
}

func nullColumn(columnType element.ColumnType) element.Column {
	switch columnType {
	case element.TypeLong:
		return element.NewNullLongColumn()
	case element.TypeDouble:
		return element.NewNullDoubleColumn()
	case element.TypeDate:
		return element.NewNullDateColumn()
	case element.TypeBool:
		return element.NewNullBoolColumn()
	case element.TypeBytes:
		return element.NewNullBytesColumn()
	default:
		return element.NewStringColumn("")
	}
}
