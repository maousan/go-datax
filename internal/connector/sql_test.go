package connector

import (
	"strings"
	"testing"
)

func TestDatabaseTypeMapping(t *testing.T) {
	mysqlCases := map[string]DataType{
		"BIGINT":          TypeInt64,
		"UNSIGNED BIGINT": TypeInt64,
		"DECIMAL":         TypeDecimal,
		"DOUBLE":          TypeFloat64,
		"VARCHAR":         TypeString,
		"VARBINARY":       TypeBytes,
		"BOOLEAN":         TypeBool,
		"DATETIME":        TypeTimestamp,
		"TIME":            TypeString,
	}
	for raw, want := range mysqlCases {
		if got := mapMySQLType(raw); got != want {
			t.Errorf("mapMySQLType(%q) = %s, want %s", raw, got, want)
		}
	}
	postgresCases := map[string]DataType{
		"INT8":        TypeInt64,
		"NUMERIC":     TypeDecimal,
		"FLOAT8":      TypeFloat64,
		"TEXT":        TypeString,
		"BYTEA":       TypeBytes,
		"BOOL":        TypeBool,
		"TIMESTAMPTZ": TypeTimestamp,
		"TIME":        TypeString,
		"UUID":        TypeString,
	}
	for raw, want := range postgresCases {
		if got := mapPostgresType(raw); got != want {
			t.Errorf("mapPostgresType(%q) = %s, want %s", raw, got, want)
		}
	}
}

func TestUpsertSQL(t *testing.T) {
	schema := Schema{
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
	}
	mysql := mysqlUpsertSQL("db.orders", schema, []string{"id"})
	for _, wanted := range []string{
		"INSERT INTO `db`.`orders` (`id`, `name`) VALUES (?, ?)",
		"ON DUPLICATE KEY UPDATE `name`=VALUES(`name`)",
	} {
		if !strings.Contains(mysql, wanted) {
			t.Errorf("MySQL upsert missing %q: %s", wanted, mysql)
		}
	}
	postgres := postgresUpsertSQL("public.orders", schema, []string{"id"})
	for _, wanted := range []string{
		`INSERT INTO "public"."orders" ("id", "name") VALUES ($1, $2)`,
		`ON CONFLICT ("id") DO UPDATE SET "name"=EXCLUDED."name"`,
	} {
		if !strings.Contains(postgres, wanted) {
			t.Errorf("PostgreSQL upsert missing %q: %s", wanted, postgres)
		}
	}

	composite := Schema{
		{Name: "tenant", Type: TypeString},
		{Name: "id", Type: TypeInt64},
		{Name: "name", Type: TypeString},
	}
	if got := postgresUpsertSQL("t", composite, []string{"tenant", "id"}); !strings.Contains(got, `ON CONFLICT ("tenant", "id") DO UPDATE SET "name"=EXCLUDED."name"`) {
		t.Errorf("PostgreSQL composite upsert = %s", got)
	}
	if got := mysqlUpsertSQL("t", composite, []string{"tenant", "id"}); !strings.Contains(got, "ON DUPLICATE KEY UPDATE `name`=VALUES(`name`)") {
		t.Errorf("MySQL composite upsert = %s", got)
	}

	single := Schema{{Name: "id", Type: TypeInt64}}
	if got := mysqlUpsertSQL("t", single, []string{"id"}); !strings.Contains(got, "`id`=`id`") {
		t.Errorf("MySQL all-key upsert = %s", got)
	}
	if got := postgresUpsertSQL("t", single, []string{"id"}); !strings.Contains(got, "DO NOTHING") {
		t.Errorf("PostgreSQL all-key upsert = %s", got)
	}
}

func TestValidateDatabaseConnector(t *testing.T) {
	cfg := Config{
		Connector:      "mysql",
		DataSourceType: "mysql",
		JDBCUrl:        "jdbc:mysql://localhost/app",
		Schema: Schema{
			{Name: "id", Type: TypeInt64},
			{Name: "name", Type: TypeString},
		},
		Options: map[string]string{"table": "orders", "writeMode": "upsert"},
	}
	if err := ValidateSink(cfg); err != nil {
		t.Fatalf("ValidateSink() failed: %v", err)
	}
	if err := ValidateSource(cfg); err == nil {
		t.Fatal("source accepted writeMode")
	}
	cfg.Options = map[string]string{"table": "orders", "splitPk": "id"}
	if err := ValidateSource(cfg); err != nil {
		t.Fatalf("ValidateSource() failed: %v", err)
	}
	cfg.Options = map[string]string{"table": "orders", "splitPk": "name"}
	if err := ValidateSource(cfg); err == nil || !strings.Contains(err.Error(), "int64") {
		t.Fatalf("string splitPk error = %v", err)
	}
	cfg.Schema = nil
	if err := validateConnectorConfig(cfg); err != nil {
		t.Fatalf("discovery preflight rejected splitPk without schema: %v", err)
	}
	cfg.Options = map[string]string{"query": "SELECT 1", "splitPk": "id"}
	if err := ValidateSource(cfg); err == nil {
		t.Fatal("query mode accepted splitPk")
	}
}

func TestValidatePostgresRequiresMatchingDataSource(t *testing.T) {
	cfg := Config{
		Connector:      "postgres",
		DataSourceType: "mysql",
		JDBCUrl:        "jdbc:postgresql://localhost/app",
		Schema:         Schema{{Name: "id", Type: TypeInt64}},
		Options:        map[string]string{"table": "orders"},
	}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected data source type mismatch")
	}
	cfg.DataSourceType = "postgres"
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}
