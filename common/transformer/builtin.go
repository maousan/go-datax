package transformer

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/longkeyy/go-datax/common/element"
)

func init() {
	Register("dx_substr", newSubstrTransformer)
	Register("dx_pad", newPadTransformer)
	Register("dx_replace", newReplaceTransformer)
	Register("dx_filter", newFilterTransformer)
	Register("dx_digest", newDigestTransformer)
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// columnAt resolves a column and rejects out-of-range indexes.
func columnAt(record element.Record, index int) (element.Column, error) {
	if index < 0 || index >= record.GetColumnNumber() {
		return nil, fmt.Errorf("column index %d out of range (record has %d columns)", index, record.GetColumnNumber())
	}
	return record.GetColumn(index), nil
}

func isNullColumn(column element.Column) bool {
	return column == nil || column.IsNull()
}

func setStringColumn(record element.Record, index int, value string) {
	record.SetColumn(index, element.NewStringColumn(value))
}

func parseNonNegativeInt(raw, name string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", name, raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative, got %d", name, value)
	}
	return value, nil
}

// repeatRunes repeats unit until exactly count runes are produced.
func repeatRunes(unit string, count int) string {
	if count <= 0 {
		return ""
	}
	runes := []rune(unit)
	out := make([]rune, 0, count)
	for len(out) < count {
		out = append(out, runes...)
	}
	return string(out[:count])
}

// ---------------------------------------------------------------------------
// dx_substr
// ---------------------------------------------------------------------------

type substrTransformer struct {
	columnIndex int
	pos         int
	length      int
}

func newSubstrTransformer(columnIndex int, paras []string) (Transformer, error) {
	if len(paras) != 2 {
		return nil, fmt.Errorf("dx_substr requires 2 parameters [pos, length], got %d", len(paras))
	}
	pos, err := parseNonNegativeInt(paras[0], "pos")
	if err != nil {
		return nil, fmt.Errorf("dx_substr: %v", err)
	}
	length, err := parseNonNegativeInt(paras[1], "length")
	if err != nil {
		return nil, fmt.Errorf("dx_substr: %v", err)
	}
	return &substrTransformer{columnIndex: columnIndex, pos: pos, length: length}, nil
}

func (t *substrTransformer) Name() string { return "dx_substr" }

func (t *substrTransformer) Evaluate(record element.Record) (element.Record, error) {
	column, err := columnAt(record, t.columnIndex)
	if err != nil {
		return nil, fmt.Errorf("dx_substr: %v", err)
	}
	if isNullColumn(column) {
		return record, nil
	}

	runes := []rune(column.GetAsString())
	if t.pos > len(runes) {
		return nil, fmt.Errorf("dx_substr: start position %d out of range (value length %d)", t.pos, len(runes))
	}
	end := len(runes)
	if t.length < len(runes)-t.pos {
		end = t.pos + t.length
	}

	setStringColumn(record, t.columnIndex, string(runes[t.pos:end]))
	return record, nil
}

// ---------------------------------------------------------------------------
// dx_pad
// ---------------------------------------------------------------------------

type padTransformer struct {
	columnIndex int
	padLeft     bool
	length      int
	pad         string
}

func newPadTransformer(columnIndex int, paras []string) (Transformer, error) {
	if len(paras) != 3 {
		return nil, fmt.Errorf("dx_pad requires 3 parameters [flag, length, chr], got %d", len(paras))
	}

	var padLeft bool
	switch strings.ToLower(strings.TrimSpace(paras[0])) {
	case "l", "left":
		padLeft = true
	case "r", "right":
		padLeft = false
	default:
		return nil, fmt.Errorf("dx_pad: flag must be one of l, left, r, right, got %q", paras[0])
	}

	length, err := parseNonNegativeInt(paras[1], "length")
	if err != nil {
		return nil, fmt.Errorf("dx_pad: %v", err)
	}
	if paras[2] == "" {
		return nil, fmt.Errorf("dx_pad: chr must not be empty")
	}

	return &padTransformer{
		columnIndex: columnIndex,
		padLeft:     padLeft,
		length:      length,
		pad:         paras[2],
	}, nil
}

func (t *padTransformer) Name() string { return "dx_pad" }

func (t *padTransformer) Evaluate(record element.Record) (element.Record, error) {
	column, err := columnAt(record, t.columnIndex)
	if err != nil {
		return nil, fmt.Errorf("dx_pad: %v", err)
	}

	// Addax semantics: null is treated as an empty string, so the result is
	// made of padding characters only.
	if isNullColumn(column) {
		setStringColumn(record, t.columnIndex, repeatRunes(t.pad, t.length))
		return record, nil
	}

	runes := []rune(column.GetAsString())
	if len(runes) >= t.length {
		// Over-length values are always truncated on the right.
		setStringColumn(record, t.columnIndex, string(runes[:t.length]))
		return record, nil
	}

	padding := repeatRunes(t.pad, t.length-len(runes))
	if t.padLeft {
		setStringColumn(record, t.columnIndex, padding+string(runes))
	} else {
		setStringColumn(record, t.columnIndex, string(runes)+padding)
	}
	return record, nil
}

// ---------------------------------------------------------------------------
// dx_replace
// ---------------------------------------------------------------------------

type replaceTransformer struct {
	columnIndex int

	// positional mode (Addax): replace [pos, pos+length) with str.
	positional bool
	pos        int
	length     int
	str        string

	// legacy mode: replace every occurrence of search with replacement.
	search      string
	replacement string
}

func newReplaceTransformer(columnIndex int, paras []string) (Transformer, error) {
	switch len(paras) {
	case 2:
		// Legacy go-datax form kept for backwards compatibility with existing
		// job files: dx_replace(idx, "search", "replacement").
		if paras[0] == "" {
			return nil, fmt.Errorf("dx_replace: search string must not be empty")
		}
		return &replaceTransformer{
			columnIndex: columnIndex,
			search:      paras[0],
			replacement: paras[1],
		}, nil
	case 3:
		// Addax form: dx_replace(idx, pos, length, str).
		pos, err := parseNonNegativeInt(paras[0], "pos")
		if err != nil {
			return nil, fmt.Errorf("dx_replace: %v", err)
		}
		length, err := parseNonNegativeInt(paras[1], "length")
		if err != nil {
			return nil, fmt.Errorf("dx_replace: %v", err)
		}
		return &replaceTransformer{
			columnIndex: columnIndex,
			positional:  true,
			pos:         pos,
			length:      length,
			str:         paras[2],
		}, nil
	default:
		return nil, fmt.Errorf("dx_replace requires 2 parameters [search, replacement] or 3 parameters [pos, length, str], got %d", len(paras))
	}
}

func (t *replaceTransformer) Name() string { return "dx_replace" }

func (t *replaceTransformer) Evaluate(record element.Record) (element.Record, error) {
	column, err := columnAt(record, t.columnIndex)
	if err != nil {
		return nil, fmt.Errorf("dx_replace: %v", err)
	}
	if isNullColumn(column) {
		return record, nil
	}

	value := column.GetAsString()
	if !t.positional {
		setStringColumn(record, t.columnIndex, strings.ReplaceAll(value, t.search, t.replacement))
		return record, nil
	}

	runes := []rune(value)
	if t.pos > len(runes) {
		return nil, fmt.Errorf("dx_replace: start position %d out of range (value length %d)", t.pos, len(runes))
	}
	end := len(runes)
	if t.length < len(runes)-t.pos {
		end = t.pos + t.length
	}

	var builder strings.Builder
	builder.WriteString(string(runes[:t.pos]))
	builder.WriteString(t.str)
	builder.WriteString(string(runes[end:]))
	setStringColumn(record, t.columnIndex, builder.String())
	return record, nil
}

// ---------------------------------------------------------------------------
// dx_filter
// ---------------------------------------------------------------------------

type filterTransformer struct {
	columnIndex int
	operator    string
	expr        string
	regex       *regexp.Regexp
}

func newFilterTransformer(columnIndex int, paras []string) (Transformer, error) {
	if len(paras) != 2 {
		return nil, fmt.Errorf("dx_filter requires 2 parameters [operator, expr], got %d", len(paras))
	}

	operator := normalizeOperator(paras[0])
	expr := paras[1]
	if expr == "" {
		return nil, fmt.Errorf("dx_filter: expr must not be empty")
	}

	filter := &filterTransformer{
		columnIndex: columnIndex,
		operator:    operator,
		expr:        expr,
	}

	switch operator {
	case "like", "not like":
		// Addax performs a full match against the expression, which it treats
		// as a regular expression.
		regex, err := regexp.Compile("^(?:" + expr + ")$")
		if err != nil {
			return nil, fmt.Errorf("dx_filter: invalid regular expression %q: %v", expr, err)
		}
		filter.regex = regex
	case "=", "==", "!=", ">", ">=", "<", "<=":
		// Validated below per column type when records are evaluated.
	default:
		return nil, fmt.Errorf("dx_filter: unsupported operator %q (supported: like, not like, >, =, <, >=, !=, <=)", paras[0])
	}

	return filter, nil
}

func normalizeOperator(raw string) string {
	operator := strings.ToLower(strings.TrimSpace(raw))
	return strings.Join(strings.Fields(operator), " ")
}

func (t *filterTransformer) Name() string { return "dx_filter" }

func (t *filterTransformer) Evaluate(record element.Record) (element.Record, error) {
	column, err := columnAt(record, t.columnIndex)
	if err != nil {
		return nil, fmt.Errorf("dx_filter: %v", err)
	}

	switch t.operator {
	case "like":
		// Null never matches like, so it is kept.
		if isNullColumn(column) || !t.regex.MatchString(column.GetAsString()) {
			return record, nil
		}
		return nil, nil
	case "not like":
		// Null satisfies not like, so it is filtered.
		if isNullColumn(column) || !t.regex.MatchString(column.GetAsString()) {
			return nil, nil
		}
		return record, nil
	}

	// Null handling for comparisons follows Addax: "= null" matches, while
	// "!= null" (and other operators) keep the record, with != implemented as
	// the inversion of = for non-null values.
	matched, err := t.matches(column)
	if err != nil {
		return nil, fmt.Errorf("dx_filter: %v", err)
	}
	if matched {
		return nil, nil
	}
	return record, nil
}

func (t *filterTransformer) matches(column element.Column) (bool, error) {
	if isNullColumn(column) {
		switch t.operator {
		case "=", "==":
			return strings.EqualFold(t.expr, "null"), nil
		case "!=":
			// "!= null" keeps null; any other value filters it (inversion of =).
			return !strings.EqualFold(t.expr, "null"), nil
		default:
			return false, nil
		}
	}

	var cmp int
	switch column.GetType() {
	case element.TypeLong, element.TypeDate:
		actual, err := column.GetAsLong()
		if err != nil {
			return false, fmt.Errorf("cannot read column %d as number: %v", t.columnIndex, err)
		}
		expected, err := strconv.ParseInt(strings.TrimSpace(t.expr), 10, 64)
		if err != nil {
			return false, fmt.Errorf("cannot parse expr %q as integer for column %d: %v", t.expr, t.columnIndex, err)
		}
		cmp = compareOrdered(actual, expected)
	case element.TypeDouble:
		actual, err := column.GetAsDouble()
		if err != nil {
			return false, fmt.Errorf("cannot read column %d as number: %v", t.columnIndex, err)
		}
		expected, err := strconv.ParseFloat(strings.TrimSpace(t.expr), 64)
		if err != nil {
			return false, fmt.Errorf("cannot parse expr %q as number for column %d: %v", t.expr, t.columnIndex, err)
		}
		cmp = compareOrdered(actual, expected)
	default:
		// String, bool and bytes columns compare lexicographically.
		actual := column.GetAsString()
		cmp = strings.Compare(actual, t.expr)
	}

	switch t.operator {
	case "=", "==":
		return cmp == 0, nil
	case "!=":
		return cmp != 0, nil
	case ">":
		return cmp > 0, nil
	case ">=":
		return cmp >= 0, nil
	case "<":
		return cmp < 0, nil
	case "<=":
		return cmp <= 0, nil
	default:
		return false, fmt.Errorf("unsupported operator %q", t.operator)
	}
}

func compareOrdered[T int64 | float64](actual, expected T) int {
	switch {
	case actual < expected:
		return -1
	case actual > expected:
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// dx_digest (go-datax extension, not part of Addax)
// ---------------------------------------------------------------------------

type digestTransformer struct {
	columnIndex int
	algorithm   string
}

func newDigestTransformer(columnIndex int, paras []string) (Transformer, error) {
	if len(paras) != 1 {
		return nil, fmt.Errorf("dx_digest requires 1 parameter [algorithm], got %d", len(paras))
	}
	algorithm := strings.ToLower(strings.TrimSpace(paras[0]))
	switch algorithm {
	case "md5", "sha1", "sha256":
	default:
		return nil, fmt.Errorf("dx_digest: unsupported algorithm %q (supported: md5, sha1, sha256)", paras[0])
	}
	return &digestTransformer{columnIndex: columnIndex, algorithm: algorithm}, nil
}

func (t *digestTransformer) Name() string { return "dx_digest" }

func (t *digestTransformer) Evaluate(record element.Record) (element.Record, error) {
	column, err := columnAt(record, t.columnIndex)
	if err != nil {
		return nil, fmt.Errorf("dx_digest: %v", err)
	}
	if isNullColumn(column) {
		return record, nil
	}

	value := column.GetAsString()
	var digest string
	switch t.algorithm {
	case "md5":
		sum := md5.Sum([]byte(value))
		digest = fmt.Sprintf("%x", sum)
	case "sha1":
		sum := sha1.Sum([]byte(value))
		digest = fmt.Sprintf("%x", sum)
	case "sha256":
		sum := sha256.Sum256([]byte(value))
		digest = fmt.Sprintf("%x", sum)
	}

	setStringColumn(record, t.columnIndex, digest)
	return record, nil
}
