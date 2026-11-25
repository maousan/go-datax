package connector

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCSVDelimiter = ','
	defaultTXVDelimiter = '\t'
	discoverySampleSize = 100
	maxLineBytes        = 16 << 20
)

func validateFile(cfg Config) error {
	connector := strings.ToLower(strings.TrimSpace(cfg.Connector))
	switch connector {
	case "csv", "txt", "jsonl":
	default:
		return fmt.Errorf("unsupported file connector %q", cfg.Connector)
	}
	if err := validateOptionKeys(cfg.Options, "path", "delimiter", "header", "nullFormat", "writeMode"); err != nil {
		return err
	}
	if _, err := resolveFilePath(cfg); err != nil {
		return err
	}
	if _, err := fileDelimiter(cfg); err != nil {
		return err
	}
	if connector != "jsonl" {
		if _, err := fileHeaderOption(cfg); err != nil {
			return err
		}
	}
	if mode := option(cfg.Options, "writeMode", "insert"); mode != "insert" && mode != "truncate" {
		return fmt.Errorf("writeMode must be insert or truncate")
	}
	return nil
}

func validateFileSource(cfg Config) error {
	return rejectTableAndQuery(cfg)
}

func validateFileSink(cfg Config) error {
	return rejectTableAndQuery(cfg)
}

func rejectTableAndQuery(cfg Config) error {
	if strings.TrimSpace(cfg.Options["table"]) != "" || strings.TrimSpace(cfg.Options["query"]) != "" {
		return fmt.Errorf("file connectors use path, not table or query")
	}
	return nil
}

// resolveFilePath maps the source/sink path against the DataSource basePath and
// rejects absolute paths or paths that escape the root.
func resolveFilePath(cfg Config) (string, error) {
	base := strings.TrimSpace(cfg.DataSourceProperties["basePath"])
	if base == "" {
		return "", fmt.Errorf("filesystem data source requires basePath")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("filesystem basePath must be absolute")
	}
	relative, err := requiredOption(cfg.Options, "path")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path must be relative to the data source basePath")
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path must stay inside the data source basePath")
	}
	return targetAbs, nil
}

func fileHeaderOption(cfg Config) (bool, error) {
	fallback := strings.EqualFold(cfg.Connector, "csv")
	return boolOption(cfg.Options, "header", fallback)
}

func fileDelimiter(cfg Config) (rune, error) {
	if strings.EqualFold(cfg.Connector, "txt") {
		return delimiterOption(cfg.Options, defaultTXVDelimiter)
	}
	delimiter, err := delimiterOption(cfg.Options, defaultCSVDelimiter)
	if err != nil {
		return 0, err
	}
	if strings.EqualFold(cfg.Connector, "csv") && delimiter == '"' {
		return 0, fmt.Errorf("csv delimiter must not be a quote")
	}
	return delimiter, nil
}

func newLineScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)
	return scanner
}

func nextNonBlankLine(scanner *bufio.Scanner) (string, error) {
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		return line, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}

type fileSource struct {
	connector  string
	file       *os.File
	schema     Schema
	header     bool
	delimiter  rune
	nullFormat string
	csv        *csv.Reader
	scanner    *bufio.Scanner
	mu         sync.Mutex
	started    bool
	eof        bool
	closed     bool
	row        int
}

func newFileSource(cfg Config) (Source, error) {
	path, err := resolveFilePath(cfg)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open source file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("%s is a directory", path)
	}
	connector := strings.ToLower(strings.TrimSpace(cfg.Connector))
	header, err := fileHeaderOption(cfg)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	delimiter, err := fileDelimiter(cfg)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	source := &fileSource{
		connector:  connector,
		file:       file,
		schema:     cfg.Schema,
		header:     header,
		delimiter:  delimiter,
		nullFormat: option(cfg.Options, "nullFormat", ""),
	}
	switch connector {
	case "csv":
		reader := csv.NewReader(file)
		reader.Comma = delimiter
		reader.FieldsPerRecord = len(cfg.Schema)
		source.csv = reader
	case "jsonl":
		source.header = false
		source.scanner = newLineScanner(file)
	default:
		source.scanner = newLineScanner(file)
	}
	return source, nil
}

func (s *fileSource) Schema() Schema { return s.schema }

func (s *fileSource) Splits(ctx context.Context) ([]Split, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []Split{{ID: "0"}}, nil
}

func (s *fileSource) ReadBatch(ctx context.Context, split Split) (Batch, error) {
	if split.ID != "0" {
		return Batch{}, fmt.Errorf("unknown file split %q", split.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eof || s.closed {
		return Batch{}, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return Batch{}, err
	}
	if !s.started {
		s.started = true
		if s.header {
			if _, err := s.readFields(); err != nil {
				s.eof = true
				if err == io.EOF {
					return Batch{}, io.EOF
				}
				return Batch{}, fmt.Errorf("read header: %w", err)
			}
		}
	}

	batch := Batch{Schema: s.schema}
	for len(batch.Rows) < defaultBatchSize {
		if err := ctx.Err(); err != nil {
			return Batch{}, err
		}
		row, err := s.readRow()
		if err == io.EOF {
			s.eof = true
			break
		}
		if err != nil {
			s.eof = true
			return Batch{}, &RowError{Row: s.row, Err: err}
		}
		s.row++
		batch.Rows = append(batch.Rows, row)
		batch.Bytes += estimateRowBytes(row)
	}
	if len(batch.Rows) == 0 {
		return Batch{}, io.EOF
	}
	return batch, nil
}

func (s *fileSource) readRow() ([]any, error) {
	if s.connector == "jsonl" {
		line, err := nextNonBlankLine(s.scanner)
		if err != nil {
			return nil, err
		}
		object, _, err := decodeJSONRecord(line)
		if err != nil {
			return nil, err
		}
		return s.convertJSONRecord(object)
	}
	fields, err := s.readFields()
	if err != nil {
		return nil, err
	}
	return s.convertFields(fields)
}

func (s *fileSource) readFields() ([]string, error) {
	switch s.connector {
	case "csv":
		return s.csv.Read()
	case "txt":
		line, err := nextNonBlankLine(s.scanner)
		if err != nil {
			return nil, err
		}
		return strings.Split(line, string(s.delimiter)), nil
	default:
		return nil, fmt.Errorf("connector %s is not delimited", s.connector)
	}
}

func (s *fileSource) convertFields(fields []string) ([]any, error) {
	if len(fields) != len(s.schema) {
		return nil, fmt.Errorf("expected %d columns, got %d", len(s.schema), len(fields))
	}
	row := make([]any, len(fields))
	for index, field := range fields {
		if field == s.nullFormat {
			row[index] = nil
			continue
		}
		value, err := coerceValue(field, s.schema[index].Type)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", s.schema[index].Name, err)
		}
		row[index] = value
	}
	return row, nil
}

func (s *fileSource) convertJSONRecord(object map[string]any) ([]any, error) {
	row := make([]any, len(s.schema))
	for index, column := range s.schema {
		value, ok := object[column.Name]
		if !ok || value == nil {
			row[index] = nil
			continue
		}
		converted, err := coerceJSONValue(value, column.Type)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", column.Name, err)
		}
		row[index] = converted
	}
	return row, nil
}

func (s *fileSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.eof = true
	return s.file.Close()
}

func coerceJSONValue(value any, dataType DataType) (any, error) {
	switch dataType {
	case TypeInt64:
		switch typed := value.(type) {
		case json.Number:
			return coerceValue(string(typed), TypeInt64)
		case string:
			return coerceValue(typed, TypeInt64)
		case float64:
			return coerceValue(typed, TypeInt64)
		}
	case TypeFloat64:
		switch typed := value.(type) {
		case json.Number:
			return coerceValue(string(typed), TypeFloat64)
		case string:
			return coerceValue(typed, TypeFloat64)
		case float64:
			return typed, nil
		case int64:
			return float64(typed), nil
		}
	case TypeDecimal:
		switch typed := value.(type) {
		case json.Number:
			return typed.String(), nil
		case string:
			return typed, nil
		case float64:
			return coerceValue(typed, TypeDecimal)
		}
	case TypeString:
		switch typed := value.(type) {
		case string:
			return typed, nil
		case json.Number:
			return typed.String(), nil
		}
	case TypeBool:
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case string:
			return coerceValue(typed, TypeBool)
		}
	case TypeTimestamp:
		switch typed := value.(type) {
		case string:
			return coerceValue(typed, TypeTimestamp)
		}
	case TypeBytes:
		switch typed := value.(type) {
		case string:
			decoded, err := base64.StdEncoding.DecodeString(typed)
			if err != nil {
				return nil, fmt.Errorf("decode base64 bytes: %w", err)
			}
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("cannot convert %T to %s", value, dataType)
}

func decodeJSONRecord(line string) (map[string]any, []string, error) {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, nil, fmt.Errorf("expected a JSON object")
	}
	object := map[string]any{}
	order := make([]string, 0, 8)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected a string key")
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, fmt.Errorf("key %q: %w", key, err)
		}
		if _, exists := object[key]; exists {
			return nil, nil, fmt.Errorf("duplicate key %q", key)
		}
		object[key] = value
		order = append(order, key)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, nil, fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, nil, err
	}
	return object, order, nil
}

type fileSink struct {
	connector    string
	file         *os.File
	writer       *bufio.Writer
	csv          *csv.Writer
	schema       Schema
	delimiter    rune
	nullFormat   string
	needsHeader  bool
	needsNewline bool
	started      bool
	closed       bool
	mu           sync.Mutex
}

func newFileSink(cfg Config) (Sink, error) {
	path, err := resolveFilePath(cfg)
	if err != nil {
		return nil, err
	}
	connector := strings.ToLower(strings.TrimSpace(cfg.Connector))
	header, err := fileHeaderOption(cfg)
	if err != nil {
		return nil, err
	}
	delimiter, err := fileDelimiter(cfg)
	if err != nil {
		return nil, err
	}
	mode := option(cfg.Options, "writeMode", "insert")
	flags := os.O_CREATE
	if mode == "truncate" {
		flags |= os.O_WRONLY | os.O_TRUNC
	} else {
		flags |= os.O_RDWR | os.O_APPEND
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open sink file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("%s is a directory", path)
	}
	needsNewline := false
	if mode == "insert" && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, info.Size()-1); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("inspect sink file: %w", err)
		}
		needsNewline = last[0] != '\n'
	}
	sink := &fileSink{
		connector:    connector,
		file:         file,
		writer:       bufio.NewWriterSize(file, 64*1024),
		schema:       cfg.Schema,
		delimiter:    delimiter,
		nullFormat:   option(cfg.Options, "nullFormat", ""),
		needsHeader:  header && info.Size() == 0,
		needsNewline: needsNewline,
	}
	if connector == "jsonl" {
		sink.needsHeader = false
	}
	if connector == "csv" {
		writer := csv.NewWriter(sink.writer)
		writer.Comma = delimiter
		sink.csv = writer
	}
	return sink, nil
}

func (s *fileSink) WriteBatch(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("file sink is closed")
	}
	if err := batch.Validate(); err != nil {
		return err
	}
	if !schemaEqual(batch.Schema, s.schema) {
		return fmt.Errorf("batch schema does not match sink schema")
	}
	if !s.started {
		s.started = true
		if s.needsNewline {
			if _, err := s.writer.WriteString("\n"); err != nil {
				return err
			}
			s.needsNewline = false
		}
		if s.needsHeader {
			if err := s.writeHeader(); err != nil {
				return err
			}
		}
	}
	for index, row := range batch.Rows {
		if err := s.writeRow(row); err != nil {
			return &RowError{Row: index, Err: err}
		}
	}
	return s.flush()
}

func (s *fileSink) writeHeader() error {
	switch s.connector {
	case "csv":
		return s.csv.Write(s.schema.Names())
	case "txt":
		_, err := s.writer.WriteString(strings.Join(s.schema.Names(), string(s.delimiter)) + "\n")
		return err
	default:
		return nil
	}
}

func (s *fileSink) writeRow(row []any) error {
	if s.connector == "jsonl" {
		return s.writeJSONRow(row)
	}
	fields := make([]string, len(row))
	for index, value := range row {
		converted, err := coerceValue(value, s.schema[index].Type)
		if err != nil {
			return fmt.Errorf("column %s: %w", s.schema[index].Name, err)
		}
		field, err := s.formatValue(converted)
		if err != nil {
			return fmt.Errorf("column %s: %w", s.schema[index].Name, err)
		}
		fields[index] = field
	}
	if s.connector == "csv" {
		return s.csv.Write(fields)
	}
	_, err := s.writer.WriteString(strings.Join(fields, string(s.delimiter)) + "\n")
	return err
}

func (s *fileSink) writeJSONRow(row []any) error {
	var builder strings.Builder
	builder.WriteByte('{')
	for index, column := range s.schema {
		if index > 0 {
			builder.WriteByte(',')
		}
		key, err := json.Marshal(column.Name)
		if err != nil {
			return err
		}
		builder.Write(key)
		builder.WriteByte(':')
		converted, err := coerceValue(row[index], column.Type)
		if err != nil {
			return fmt.Errorf("column %s: %w", column.Name, err)
		}
		value, err := json.Marshal(converted)
		if err != nil {
			return fmt.Errorf("column %s: %w", column.Name, err)
		}
		builder.Write(value)
	}
	builder.WriteString("}\n")
	_, err := s.writer.WriteString(builder.String())
	return err
}

func (s *fileSink) formatValue(value any) (string, error) {
	if value == nil {
		return s.nullFormat, nil
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	case time.Time:
		return typed.Format(time.RFC3339Nano), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case int:
		return strconv.Itoa(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("cannot encode %T", value)
	}
}

func (s *fileSink) flush() error {
	if s.csv != nil {
		s.csv.Flush()
		if err := s.csv.Error(); err != nil {
			return err
		}
	}
	return s.writer.Flush()
}

func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	flushErr := s.flush()
	closeErr := s.file.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func discoverFile(ctx context.Context, cfg Config) (Schema, error) {
	path, err := resolveFilePath(cfg)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Connector)) {
	case "jsonl":
		return discoverJSONL(ctx, file)
	default:
		return discoverDelimited(ctx, cfg, file)
	}
}

type inferredType struct {
	typ  DataType
	seen bool
}

func (t *inferredType) add(next DataType) {
	if !t.seen {
		t.typ = next
		t.seen = true
		return
	}
	t.typ = mergeTypes(t.typ, next)
}

func (t inferredType) result() DataType {
	if !t.seen {
		return TypeString
	}
	return t.typ
}

func mergeTypes(left, right DataType) DataType {
	if left == right {
		return left
	}
	if (left == TypeInt64 && right == TypeFloat64) || (left == TypeFloat64 && right == TypeInt64) {
		return TypeFloat64
	}
	return TypeString
}

func inferFieldType(value string) DataType {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return TypeString
	}
	if strings.EqualFold(trimmed, "true") || strings.EqualFold(trimmed, "false") {
		return TypeBool
	}
	if isIntegerLiteral(trimmed) {
		return TypeInt64
	}
	if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return TypeFloat64
	}
	if _, err := parseTimestamp(trimmed); err == nil {
		return TypeTimestamp
	}
	return TypeString
}

func isIntegerLiteral(value string) bool {
	if value == "" || value[0] == '+' {
		return false
	}
	digits := value
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) > 1 && digits[0] == '0' {
		return false
	}
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

func inferJSONValue(value any) (DataType, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case bool:
		return TypeBool, nil
	case string:
		return TypeString, nil
	case json.Number:
		if _, err := typed.Int64(); err == nil {
			return TypeInt64, nil
		}
		return TypeFloat64, nil
	case []any, map[string]any:
		return "", fmt.Errorf("nested JSON values are not supported")
	default:
		return "", fmt.Errorf("unsupported JSON value %T", typed)
	}
}

func discoverDelimited(ctx context.Context, cfg Config, file *os.File) (Schema, error) {
	header, err := fileHeaderOption(cfg)
	if err != nil {
		return nil, err
	}
	delimiter, err := fileDelimiter(cfg)
	if err != nil {
		return nil, err
	}
	nullFormat := option(cfg.Options, "nullFormat", "")
	var read func() ([]string, error)
	if strings.EqualFold(cfg.Connector, "csv") {
		reader := csv.NewReader(file)
		reader.Comma = delimiter
		reader.FieldsPerRecord = -1
		read = reader.Read
	} else {
		scanner := newLineScanner(file)
		read = func() ([]string, error) {
			line, err := nextNonBlankLine(scanner)
			if err != nil {
				return nil, err
			}
			return strings.Split(line, string(delimiter)), nil
		}
	}

	var names []string
	width := 0
	if header {
		record, err := read()
		if err == io.EOF {
			return nil, fmt.Errorf("file is empty")
		}
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		if len(record) == 0 {
			return nil, fmt.Errorf("header has no columns")
		}
		names = normalizeHeader(record)
		width = len(record)
	}

	types := make([]inferredType, width)
	rows := 0
	for rows < discoverySampleSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row %d: %w", rows+1, err)
		}
		if width == 0 {
			width = len(record)
			if width == 0 {
				return nil, fmt.Errorf("row %d has no columns", rows+1)
			}
			names = generatedNames(width)
			types = make([]inferredType, width)
		}
		if len(record) != width {
			return nil, fmt.Errorf("row %d has %d columns, expected %d", rows+1, len(record), width)
		}
		for index, field := range record {
			if field == nullFormat {
				continue
			}
			types[index].add(inferFieldType(field))
		}
		rows++
	}
	if width == 0 {
		return nil, fmt.Errorf("file has no columns")
	}
	schema := make(Schema, width)
	for index := 0; index < width; index++ {
		schema[index] = Column{Name: names[index], Type: types[index].result()}
	}
	return schema, schema.Validate()
}

func discoverJSONL(ctx context.Context, file *os.File) (Schema, error) {
	scanner := newLineScanner(file)
	type column struct {
		name     string
		inferred inferredType
	}
	columns := []column{}
	indexes := map[string]int{}
	rows := 0
	for rows < discoverySampleSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := nextNonBlankLine(scanner)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		object, order, err := decodeJSONRecord(line)
		if err != nil {
			return nil, fmt.Errorf("read row %d: %w", rows+1, err)
		}
		for _, key := range order {
			inferred, err := inferJSONValue(object[key])
			if err != nil {
				return nil, fmt.Errorf("row %d key %q: %w", rows+1, key, err)
			}
			if inferred == "" {
				continue
			}
			position, ok := indexes[key]
			if !ok {
				position = len(columns)
				indexes[key] = position
				columns = append(columns, column{name: key})
			}
			columns[position].inferred.add(inferred)
		}
		rows++
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("file has no columns")
	}
	schema := make(Schema, len(columns))
	for index, item := range columns {
		schema[index] = Column{Name: item.name, Type: item.inferred.result()}
	}
	return schema, schema.Validate()
}

func normalizeHeader(record []string) []string {
	names := make([]string, len(record))
	for index, raw := range record {
		name := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if name == "" {
			name = fmt.Sprintf("column_%d", index+1)
		}
		names[index] = name
	}
	return names
}

func generatedNames(width int) []string {
	names := make([]string, width)
	for index := range names {
		names[index] = fmt.Sprintf("column_%d", index+1)
	}
	return names
}
