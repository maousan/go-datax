package connector

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func filesystemConfig(base, connector string, options map[string]string) Config {
	return Config{
		Connector:            connector,
		DataSourceType:       "filesystem",
		DataSourceProperties: map[string]string{"basePath": base},
		Options:              options,
	}
}

func writeFixture(t *testing.T, base, name, content string) string {
	t.Helper()
	path := filepath.Join(base, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readAll(t *testing.T, source Source) [][]any {
	t.Helper()
	ctx := context.Background()
	splits, err := source.Splits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]any
	for _, split := range splits {
		for {
			batch, err := source.ReadBatch(ctx, split)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, batch.Rows...)
		}
	}
	return rows
}

func sampleSchema() Schema {
	return Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
		{Name: "score", Type: TypeFloat64},
		{Name: "active", Type: TypeBool},
		{Name: "created", Type: TypeTimestamp},
	}
}

func TestCoerceUintValues(t *testing.T) {
	if value, err := CoerceValue(uint64(42), TypeInt64); err != nil || value != int64(42) {
		t.Fatalf("uint64 -> int64 = %#v, %v", value, err)
	}
	if _, err := CoerceValue(uint64(1<<63), TypeInt64); err == nil {
		t.Fatal("uint64 overflow was accepted")
	}
	if value, err := CoerceValue(uint64(42), TypeFloat64); err != nil || value != float64(42) {
		t.Fatalf("uint64 -> float64 = %#v, %v", value, err)
	}
}
