package connector

import (
	"reflect"
	"testing"
)

func TestSupportedConnectors(t *testing.T) {
	expected := []string{"csv", "jsonl", "mysql", "postgres", "sqlite", "txt"}
	if got := Supported(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("Supported() = %v, want %v", got, expected)
	}
}

func TestValidateRejectsEmptySchema(t *testing.T) {
	cfg := filesystemConfig(t.TempDir(), "csv", map[string]string{"path": "file.csv"})
	if err := Validate(cfg); err == nil {
		t.Fatal("expected empty schema to be rejected")
	}
}

func TestValidateRejectsDataSourceTypeMismatch(t *testing.T) {
	cfg := Config{
		Connector:            "sqlite",
		DataSourceType:       "filesystem",
		DataSourceProperties: map[string]string{"basePath": t.TempDir()},
		Schema:               sampleSchema(),
		Options:              map[string]string{"table": "t"},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected data source type mismatch")
	}
}

func TestValidateUnknownConnector(t *testing.T) {
	cfg := Config{
		Connector:      "parquet",
		DataSourceType: "filesystem",
		Schema:         sampleSchema(),
		Options:        map[string]string{"path": "file.parquet"},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected unsupported connector error")
	}
}

func TestSchemaValidate(t *testing.T) {
	for name, schema := range map[string]Schema{
		"empty":            {},
		"empty name":       {{Name: "", Type: TypeString}},
		"duplicate":        {{Name: "id", Type: TypeInt64}, {Name: "ID", Type: TypeInt64}},
		"unsupported type": {{Name: "id", Type: "uuid"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := schema.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestBatchValidateReportsRow(t *testing.T) {
	err := Batch{
		Schema: Schema{{Name: "id", Type: TypeInt64}},
		Rows:   [][]any{{int64(1), "extra"}},
	}.Validate()
	if err == nil {
		t.Fatal("expected row width error")
	}
}
