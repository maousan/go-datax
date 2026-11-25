package connector

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const defaultSplitCount = 4

// sqlDialect captures the differences between database/sql backed connectors.
type sqlDialect struct {
	name        string
	driver      string
	quote       func(string) string
	placeholder func(int) string
	mapType     func(string) DataType
	open        func(Config) (string, string, error)
	driverValue func(any) any
	upsertSQL   func(table string, schema Schema, primaryKey []string) string
	primaryKey  func(context.Context, *sql.DB, string) ([]string, error)
	truncateSQL func(string) string
}

func validateSQLConnector(cfg Config, dialect sqlDialect) error {
	if err := validateOptionKeys(cfg.Options, "table", "query", "splitPk", "writeMode"); err != nil {
		return err
	}
	table := strings.TrimSpace(cfg.Options["table"])
	query := strings.TrimSpace(cfg.Options["query"])
	if table != "" && query != "" {
		return fmt.Errorf("%s connector accepts either table or query, not both", dialect.name)
	}
	if table == "" && query == "" {
		return fmt.Errorf("%s connector requires table or query", dialect.name)
	}
	if splitPk := strings.TrimSpace(cfg.Options["splitPk"]); splitPk != "" {
		if table == "" {
			return fmt.Errorf("splitPk requires table mode")
		}
		// Discovery runs before a schema exists; NewSource always supplies one.
		if len(cfg.Schema) > 0 {
			index := cfg.Schema.Index(splitPk)
			if index < 0 {
				return fmt.Errorf("splitPk column %q is not in the schema", splitPk)
			}
			if cfg.Schema[index].Type != TypeInt64 {
				return fmt.Errorf("splitPk column %q must be int64", splitPk)
			}
		}
	}
	if mode := strings.TrimSpace(cfg.Options["writeMode"]); mode != "" {
		if mode != "insert" && mode != "truncate" && !(mode == "upsert" && dialect.upsertSQL != nil) {
			return fmt.Errorf("writeMode must be insert, truncate or upsert")
		}
	}
	return nil
}

func validateSQLSource(cfg Config, dialect sqlDialect) error {
	if strings.TrimSpace(cfg.Options["writeMode"]) != "" {
		return fmt.Errorf("writeMode is only supported by sinks")
	}
	return nil
}

func validateSQLSink(cfg Config, dialect sqlDialect) error {
	if strings.TrimSpace(cfg.Options["query"]) != "" {
		return fmt.Errorf("%s sink does not support query", dialect.name)
	}
	if strings.TrimSpace(cfg.Options["table"]) == "" {
		return fmt.Errorf("%s sink requires table", dialect.name)
	}
	if strings.TrimSpace(cfg.Options["splitPk"]) != "" {
		return fmt.Errorf("splitPk is only supported by sources")
	}
	return nil
}

func discoverSQL(ctx context.Context, cfg Config, dialect sqlDialect) (Schema, error) {
	driver, dsn, err := dialect.open(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	table := strings.TrimSpace(cfg.Options["table"])
	query := strings.TrimSpace(cfg.Options["query"])
	probe := "SELECT * FROM " + dialect.quote(table) + " LIMIT 0"
	if query != "" {
		normalized := strings.TrimSpace(strings.TrimSuffix(query, ";"))
		if normalized == "" {
			return nil, fmt.Errorf("query is empty")
		}
		probe = "SELECT * FROM (" + normalized + ") dx_probe LIMIT 0"
	}
	rows, err := db.QueryContext(ctx, probe)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", dialect.name, err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	schema := make(Schema, 0, len(types))
	for _, columnType := range types {
		schema = append(schema, Column{
			Name: columnType.Name(),
			Type: dialect.mapType(columnType.DatabaseTypeName()),
		})
	}
	return schema, schema.Validate()
}

type sqlCursor struct {
	rows      *sql.Rows
	read      int
	exhausted bool
}

type sqlSource struct {
	db      *sql.DB
	dialect sqlDialect
	cfg     Config
	schema  Schema
	mu      sync.Mutex
	cursors map[string]*sqlCursor
	done    map[string]bool
}

func newSQLSource(cfg Config, dialect sqlDialect) (Source, error) {
	driver, dsn, err := dialect.open(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(defaultSplitCount)
	return &sqlSource{
		db:      db,
		dialect: dialect,
		cfg:     cfg,
		schema:  cfg.Schema,
		cursors: map[string]*sqlCursor{},
		done:    map[string]bool{},
	}, nil
}

func (s *sqlSource) Schema() Schema { return s.schema }

func (s *sqlSource) Splits(ctx context.Context) ([]Split, error) {
	table := strings.TrimSpace(s.cfg.Options["table"])
	query := strings.TrimSpace(s.cfg.Options["query"])
	splitPk := strings.TrimSpace(s.cfg.Options["splitPk"])
	if index := s.schema.Index(splitPk); index >= 0 {
		splitPk = s.schema[index].Name
	}
	if query != "" || table == "" || splitPk == "" {
		sqlText := query
		if sqlText == "" {
			sqlText = "SELECT " + selectWithQuote(s.schema, s.dialect.quote) + " FROM " + s.dialect.quote(table)
		}
		return []Split{{ID: "0", SQL: sqlText}}, nil
	}

	base := "SELECT " + selectWithQuote(s.schema, s.dialect.quote) + " FROM " + s.dialect.quote(table)
	pk := s.dialect.quote(splitPk)
	var min, max sql.NullInt64
	probe := fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM %s", pk, pk, s.dialect.quote(table))
	if err := s.db.QueryRowContext(ctx, probe).Scan(&min, &max); err != nil {
		return nil, fmt.Errorf("probe split range: %w", err)
	}
	if !min.Valid || !max.Valid || max.Int64 <= min.Int64 {
		return []Split{{ID: "0", SQL: base}}, nil
	}

	const splitCount = defaultSplitCount
	span := max.Int64 - min.Int64 + 1
	if span < 1 {
		return []Split{{ID: "0", SQL: base}}, nil
	}
	step := span / splitCount
	if step < 1 {
		step = 1
	}
	splits := make([]Split, 0, splitCount)
	for index := 0; index < splitCount; index++ {
		start := min.Int64 + int64(index)*step
		if start > max.Int64 {
			break
		}
		if index == splitCount-1 {
			splits = append(splits, Split{
				ID:   strconv.Itoa(index),
				SQL:  base + " WHERE " + pk + " >= " + s.dialect.placeholder(1),
				Args: []any{start},
			})
			break
		}
		end := start + step
		sqlText := base + " WHERE " + pk + " >= " + s.dialect.placeholder(1) +
			" AND " + pk + " < " + s.dialect.placeholder(2)
		if index == 0 {
			// NULL keys do not satisfy range predicates; keep them in the first split.
			sqlText = base + " WHERE (" + pk + " >= " + s.dialect.placeholder(1) +
				" AND " + pk + " < " + s.dialect.placeholder(2) + ") OR " + pk + " IS NULL"
		}
		splits = append(splits, Split{
			ID:   strconv.Itoa(index),
			SQL:  sqlText,
			Args: []any{start, end},
		})
	}
	return splits, nil
}

func (s *sqlSource) ReadBatch(ctx context.Context, split Split) (Batch, error) {
	s.mu.Lock()
	if s.done[split.ID] {
		s.mu.Unlock()
		return Batch{}, io.EOF
	}
	cursor := s.cursors[split.ID]
	if cursor == nil {
		rows, err := s.db.QueryContext(ctx, split.SQL, split.Args...)
		if err != nil {
			s.mu.Unlock()
			return Batch{}, err
		}
		cursor = &sqlCursor{rows: rows}
		s.cursors[split.ID] = cursor
	}
	s.mu.Unlock()

	batch := Batch{Schema: s.schema}
	for len(batch.Rows) < defaultBatchSize && !cursor.exhausted {
		if !cursor.rows.Next() {
			if err := cursor.rows.Err(); err != nil {
				s.markDone(split.ID)
				return Batch{}, err
			}
			cursor.exhausted = true
			break
		}
		rowIndex := cursor.read
		row, err := scanCanonicalRow(cursor.rows, s.schema)
		if err != nil {
			s.markDone(split.ID)
			return Batch{}, &RowError{Row: rowIndex, Err: err}
		}
		cursor.read++
		batch.Rows = append(batch.Rows, row)
		batch.Bytes += estimateRowBytes(row)
	}
	if cursor.exhausted {
		s.markDone(split.ID)
	}
	if len(batch.Rows) == 0 {
		return Batch{}, io.EOF
	}
	return batch, nil
}

func (s *sqlSource) markDone(id string) {
	s.mu.Lock()
	if cursor := s.cursors[id]; cursor != nil {
		_ = cursor.rows.Close()
		delete(s.cursors, id)
	}
	s.done[id] = true
	s.mu.Unlock()
}

func (s *sqlSource) Close() error {
	s.mu.Lock()
	for id, cursor := range s.cursors {
		_ = cursor.rows.Close()
		delete(s.cursors, id)
	}
	s.done = map[string]bool{}
	s.mu.Unlock()
	return s.db.Close()
}

type sqlSink struct {
	db         *sql.DB
	dialect    sqlDialect
	table      string
	mode       string
	schema     Schema
	primaryKey []string
}

func newSQLSink(cfg Config, dialect sqlDialect) (Sink, error) {
	driver, dsn, err := dialect.open(cfg)
	if err != nil {
		return nil, err
	}
	table := strings.TrimSpace(cfg.Options["table"])
	if table == "" {
		return nil, fmt.Errorf("%s sink requires table", dialect.name)
	}
	mode := option(cfg.Options, "writeMode", "insert")
	if mode != "insert" && mode != "truncate" && !(mode == "upsert" && dialect.upsertSQL != nil) {
		return nil, fmt.Errorf("writeMode must be insert, truncate or upsert")
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	sink := &sqlSink{db: db, dialect: dialect, table: table, mode: mode, schema: cfg.Schema}
	if mode == "upsert" {
		ctx, cancel := context.WithTimeout(context.Background(), databaseConnectTimeout)
		primaryKey, err := dialect.primaryKey(ctx, db, table)
		cancel()
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("detect primary key: %w", err)
		}
		if len(primaryKey) == 0 {
			_ = db.Close()
			return nil, fmt.Errorf("upsert requires a primary key on %s", table)
		}
		sink.primaryKey = primaryKey
	}
	if mode == "truncate" {
		if _, err := db.Exec(dialect.truncateSQL(table)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("truncate table: %w", err)
		}
	}
	return sink, nil
}

func (s *sqlSink) WriteBatch(ctx context.Context, batch Batch) error {
	if err := batch.Validate(); err != nil {
		return err
	}
	if !schemaEqual(batch.Schema, s.schema) {
		return fmt.Errorf("batch schema does not match sink schema")
	}
	statement := "INSERT INTO " + s.dialect.quote(s.table) +
		" (" + strings.Join(quotedNames(s.schema, s.dialect.quote), ", ") + ")" +
		" VALUES (" + strings.Join(placeholders(len(s.schema), s.dialect.placeholder), ", ") + ")"
	if s.mode == "upsert" {
		statement = s.dialect.upsertSQL(s.table, s.schema, s.primaryKey)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	prepared, err := tx.PrepareContext(ctx, statement)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for index, row := range batch.Rows {
		values := make([]any, len(row))
		for columnIndex, value := range row {
			converted, err := coerceValue(value, s.schema[columnIndex].Type)
			if err != nil {
				_ = prepared.Close()
				_ = tx.Rollback()
				return &RowError{Row: index, Err: fmt.Errorf("column %s: %w", s.schema[columnIndex].Name, err)}
			}
			values[columnIndex] = s.dialect.driverValue(converted)
		}
		if _, err := prepared.ExecContext(ctx, values...); err != nil {
			_ = prepared.Close()
			_ = tx.Rollback()
			return &RowError{Row: index, Err: err}
		}
	}
	if err := prepared.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *sqlSink) Close() error {
	return s.db.Close()
}

func quoteWith(name, quote string) string {
	parts := strings.Split(name, ".")
	for index, part := range parts {
		parts[index] = quote + strings.ReplaceAll(part, quote, quote+quote) + quote
	}
	return strings.Join(parts, ".")
}

func selectWithQuote(schema Schema, quote func(string) string) string {
	return strings.Join(quotedNames(schema, quote), ", ")
}

func quotedNames(schema Schema, quote func(string) string) []string {
	names := make([]string, len(schema))
	for index, column := range schema {
		names[index] = quote(column.Name)
	}
	return names
}

func placeholders(count int, placeholder func(int) string) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = placeholder(index + 1)
	}
	return result
}

func questionPlaceholder(int) string { return "?" }

func dollarPlaceholder(index int) string { return "$" + strconv.Itoa(index) }

func splitTableName(table string) (string, string) {
	parts := strings.Split(table, ".")
	if len(parts) < 2 {
		return "", parts[0]
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

func primaryKeyColumns(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}
