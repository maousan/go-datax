package connector

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const defaultBatchSize = 256

func validateSQLite(cfg Config) error {
	if err := validateOptionKeys(cfg.Options, "table", "query", "splitPk", "writeMode"); err != nil {
		return err
	}
	table := strings.TrimSpace(cfg.Options["table"])
	query := strings.TrimSpace(cfg.Options["query"])
	if table != "" && query != "" {
		return fmt.Errorf("sqlite connector accepts either table or query, not both")
	}
	if table == "" && query == "" {
		return fmt.Errorf("sqlite connector requires table or query")
	}
	if splitPk := strings.TrimSpace(cfg.Options["splitPk"]); splitPk != "" && table == "" {
		return fmt.Errorf("splitPk requires table mode")
	}
	if mode := strings.TrimSpace(cfg.Options["writeMode"]); mode != "" && mode != "insert" && mode != "truncate" {
		return fmt.Errorf("writeMode must be insert or truncate")
	}
	return nil
}

func validateSQLiteSink(cfg Config) error {
	if strings.TrimSpace(cfg.Options["query"]) != "" {
		return fmt.Errorf("sqlite sink does not support query")
	}
	if strings.TrimSpace(cfg.Options["table"]) == "" {
		return fmt.Errorf("sqlite sink requires table")
	}
	if strings.TrimSpace(cfg.Options["splitPk"]) != "" {
		return fmt.Errorf("splitPk is only supported by sqlite sources")
	}
	return nil
}

func sqlitePath(cfg Config) (string, error) {
	path := strings.TrimSpace(strings.TrimPrefix(cfg.JDBCUrl, "jdbc:sqlite:"))
	if path == "" {
		path = strings.TrimSpace(cfg.DataSourceProperties["path"])
	}
	if path == "" {
		return "", fmt.Errorf("sqlite data source has no file path")
	}
	return path, nil
}

func quoteIdentifier(name string) string {
	parts := strings.Split(name, ".")
	for index, part := range parts {
		parts[index] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func selectSQL(schema Schema) string {
	names := make([]string, len(schema))
	for index, column := range schema {
		names[index] = quoteIdentifier(column.Name)
	}
	return strings.Join(names, ", ")
}

func discoverSQLite(ctx context.Context, cfg Config) (Schema, error) {
	path, err := sqlitePath(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	table := strings.TrimSpace(cfg.Options["table"])
	query := strings.TrimSpace(cfg.Options["query"])
	if query != "" {
		normalized := strings.TrimSpace(strings.TrimSuffix(query, ";"))
		if normalized == "" {
			return nil, fmt.Errorf("query is empty")
		}
		probe := "SELECT * FROM (" + normalized + ") LIMIT 0"
		rows, err := db.QueryContext(ctx, probe)
		if err != nil {
			return nil, fmt.Errorf("inspect query: %w", err)
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
				Type: mapSQLiteType(columnType.DatabaseTypeName()),
			})
		}
		return schema, schema.Validate()
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schema := Schema{}
	for rows.Next() {
		var (
			cid      int
			name     string
			rawType  string
			notNull  int
			defaultV sql.NullString
			primary  int
		)
		if err := rows.Scan(&cid, &name, &rawType, &notNull, &defaultV, &primary); err != nil {
			return nil, err
		}
		schema = append(schema, Column{Name: name, Type: mapSQLiteType(rawType)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return schema, schema.Validate()
}

func mapSQLiteType(raw string) DataType {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "int"):
		return TypeInt64
	case strings.Contains(lower, "real"), strings.Contains(lower, "floa"), strings.Contains(lower, "doub"):
		return TypeFloat64
	case strings.Contains(lower, "dec"), strings.Contains(lower, "num"), strings.Contains(lower, "money"):
		return TypeDecimal
	case strings.Contains(lower, "blob"):
		return TypeBytes
	case strings.Contains(lower, "bool"):
		return TypeBool
	case strings.Contains(lower, "date"), strings.Contains(lower, "time"):
		return TypeTimestamp
	default:
		return TypeString
	}
}

type sqliteCursor struct {
	rows      *sql.Rows
	read      int
	exhausted bool
}

type sqliteSource struct {
	db      *sql.DB
	cfg     Config
	schema  Schema
	mu      sync.Mutex
	cursors map[string]*sqliteCursor
	done    map[string]bool
}

func newSQLiteSource(cfg Config) (Source, error) {
	path, err := sqlitePath(cfg)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	return &sqliteSource{
		db:      db,
		cfg:     cfg,
		schema:  cfg.Schema,
		cursors: map[string]*sqliteCursor{},
		done:    map[string]bool{},
	}, nil
}

func (s *sqliteSource) Schema() Schema { return s.schema }

func (s *sqliteSource) Splits(ctx context.Context) ([]Split, error) {
	table := strings.TrimSpace(s.cfg.Options["table"])
	query := strings.TrimSpace(s.cfg.Options["query"])
	splitPk := strings.TrimSpace(s.cfg.Options["splitPk"])
	if query != "" || table == "" || splitPk == "" {
		sqlText := query
		if sqlText == "" {
			sqlText = "SELECT " + selectSQL(s.schema) + " FROM " + quoteIdentifier(table)
		}
		return []Split{{ID: "0", SQL: sqlText}}, nil
	}

	base := "SELECT " + selectSQL(s.schema) + " FROM " + quoteIdentifier(table)
	var min, max sql.NullInt64
	probe := fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM %s", quoteIdentifier(splitPk), quoteIdentifier(splitPk), quoteIdentifier(table))
	if err := s.db.QueryRowContext(ctx, probe).Scan(&min, &max); err != nil ||
		!min.Valid || !max.Valid || max.Int64 <= min.Int64 {
		return []Split{{ID: "0", SQL: base}}, nil
	}

	const splitCount = 4
	span := max.Int64 - min.Int64 + 1
	if span < 1 {
		// 取值范围溢出 int64，退回单分片。
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
				SQL:  base + " WHERE " + quoteIdentifier(splitPk) + " >= ?",
				Args: []any{start},
			})
			break
		}
		end := start + step
		sqlText := base + " WHERE " + quoteIdentifier(splitPk) + " >= ? AND " + quoteIdentifier(splitPk) + " < ?"
		if index == 0 {
			// NULL 不满足范围条件；并入第一个分片，避免丢失。
			sqlText = base + " WHERE (" + quoteIdentifier(splitPk) + " >= ? AND " + quoteIdentifier(splitPk) + " < ?) OR " + quoteIdentifier(splitPk) + " IS NULL"
		}
		splits = append(splits, Split{
			ID:   strconv.Itoa(index),
			SQL:  sqlText,
			Args: []any{start, end},
		})
	}
	return splits, nil
}

func (s *sqliteSource) ReadBatch(ctx context.Context, split Split) (Batch, error) {
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
		cursor = &sqliteCursor{rows: rows}
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

func (s *sqliteSource) markDone(id string) {
	s.mu.Lock()
	if cursor := s.cursors[id]; cursor != nil {
		_ = cursor.rows.Close()
		delete(s.cursors, id)
	}
	s.done[id] = true
	s.mu.Unlock()
}

func (s *sqliteSource) Close() error {
	s.mu.Lock()
	for id, cursor := range s.cursors {
		_ = cursor.rows.Close()
		delete(s.cursors, id)
	}
	s.done = map[string]bool{}
	s.mu.Unlock()
	return s.db.Close()
}

func scanCanonicalRow(rows *sql.Rows, schema Schema) ([]any, error) {
	raw := make([]any, len(schema))
	pointers := make([]any, len(schema))
	for index := range raw {
		pointers[index] = &raw[index]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	row := make([]any, len(schema))
	for index, value := range raw {
		converted, err := coerceValue(value, schema[index].Type)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", schema[index].Name, err)
		}
		row[index] = converted
	}
	return row, nil
}

type sqliteSink struct {
	db     *sql.DB
	table  string
	mode   string
	schema Schema
}

func newSQLiteSink(cfg Config) (Sink, error) {
	path, err := sqlitePath(cfg)
	if err != nil {
		return nil, err
	}
	table := strings.TrimSpace(cfg.Options["table"])
	if table == "" {
		return nil, fmt.Errorf("sqlite sink requires table")
	}
	mode := option(cfg.Options, "writeMode", "insert")
	if mode != "insert" && mode != "truncate" {
		return nil, fmt.Errorf("writeMode must be insert or truncate")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if mode == "truncate" {
		if _, err := db.Exec("DELETE FROM " + quoteIdentifier(table)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("truncate table: %w", err)
		}
	}
	return &sqliteSink{db: db, table: table, mode: mode, schema: cfg.Schema}, nil
}

func (s *sqliteSink) WriteBatch(ctx context.Context, batch Batch) error {
	if err := batch.Validate(); err != nil {
		return err
	}
	if !schemaEqual(batch.Schema, s.schema) {
		return fmt.Errorf("batch schema does not match sink schema")
	}
	names := make([]string, len(s.schema))
	placeholders := make([]string, len(s.schema))
	for index, column := range s.schema {
		names[index] = quoteIdentifier(column.Name)
		placeholders[index] = "?"
	}
	statement := "INSERT INTO " + quoteIdentifier(s.table) + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
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
			values[columnIndex] = driverValue(converted)
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

func (s *sqliteSink) Close() error {
	return s.db.Close()
}

func driverValue(value any) any {
	switch typed := value.(type) {
	case bool:
		if typed {
			return int64(1)
		}
		return int64(0)
	case time.Time:
		return typed.Format(time.RFC3339Nano)
	default:
		return value
	}
}
