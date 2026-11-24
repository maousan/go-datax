package server

import (
	"strings"
	"testing"
)

func TestBuildTemplateReaderQuery(t *testing.T) {
	cases := []struct {
		name       string
		sql        string
		columns    []string
		dataSource string
		want       string
		wantErr    string
	}{
		{
			name:       "mysql strips outer order by",
			sql:        "SELECT id, amount FROM orders WHERE created_at >= '2026-11-01' ORDER BY id",
			columns:    []string{"id", "amount"},
			dataSource: "mysql",
			want:       "SELECT `id`, `amount` FROM (SELECT id, amount FROM orders WHERE created_at >= '2026-11-01'\n) dx_template_src",
		},
		{
			name:       "sqlserver quotes with brackets",
			sql:        "SELECT [Order No] FROM [orders]",
			columns:    []string{"Order No"},
			dataSource: "mssql",
			want:       "SELECT [Order No] FROM (SELECT [Order No] FROM [orders]\n) dx_template_src",
		},
		{
			name:       "postgres default quotes",
			sql:        "SELECT id, name FROM users",
			columns:    []string{"id", "name"},
			dataSource: "postgres",
			want:       "SELECT \"id\", \"name\" FROM (SELECT id, name FROM users\n) dx_template_src",
		},
		{
			name:       "window order by is kept",
			sql:        "SELECT SUM(amount) OVER (ORDER BY id) AS total FROM orders",
			columns:    []string{"total"},
			dataSource: "postgres",
			want:       "SELECT \"total\" FROM (SELECT SUM(amount) OVER (ORDER BY id) AS total FROM orders\n) dx_template_src",
		},
		{
			name:       "nested order by is kept",
			sql:        "SELECT id FROM (SELECT id FROM users ORDER BY id) t",
			columns:    []string{"id"},
			dataSource: "sqlite",
			want:       "SELECT \"id\" FROM (SELECT id FROM (SELECT id FROM users ORDER BY id) t\n) dx_template_src",
		},
		{
			name:       "order by followed by limit is rejected",
			sql:        "SELECT id FROM users ORDER BY id LIMIT 10",
			columns:    []string{"id"},
			dataSource: "mysql",
			wantErr:    "LIMIT/OFFSET/FETCH",
		},
		{
			name:       "with is rejected",
			sql:        "WITH t AS (SELECT 1 AS id) SELECT id FROM t",
			columns:    []string{"id"},
			dataSource: "postgres",
			wantErr:    "WITH",
		},
		{
			name:       "select into is rejected",
			sql:        "SELECT id INTO backup FROM users",
			columns:    []string{"id"},
			dataSource: "mssql",
			wantErr:    "SELECT INTO",
		},
		{
			name:       "multiple statements are rejected",
			sql:        "SELECT id FROM users; DROP TABLE users",
			columns:    []string{"id"},
			dataSource: "mysql",
			wantErr:    "一条 SQL",
		},
		{
			name:       "trailing semicolon is allowed",
			sql:        "SELECT id FROM users;",
			columns:    []string{"id"},
			dataSource: "mysql",
			want:       "SELECT `id` FROM (SELECT id FROM users\n) dx_template_src",
		},
		{
			name:       "keyword in string literal is ignored",
			sql:        "SELECT 'ORDER BY id' AS note FROM users",
			columns:    []string{"note"},
			dataSource: "mysql",
			want:       "SELECT `note` FROM (SELECT 'ORDER BY id' AS note FROM users\n) dx_template_src",
		},
		{
			name:       "trailing line comment stays outside the closing paren",
			sql:        "SELECT id FROM users -- note",
			columns:    []string{"id"},
			dataSource: "mysql",
			want:       "SELECT `id` FROM (SELECT id FROM users -- note\n) dx_template_src",
		},
		{
			name:       "empty columns are rejected",
			sql:        "SELECT id FROM users",
			columns:    nil,
			dataSource: "mysql",
			wantErr:    "读取列不能为空",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildTemplateReaderQuery(tc.sql, tc.columns, tc.dataSource)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("query =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestNormalizeTemplateSQL(t *testing.T) {
	got, err := normalizeTemplateSQL("  SELECT id FROM users;  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "SELECT id FROM users" {
		t.Errorf("normalized = %q", got)
	}

	if _, err := normalizeTemplateSQL("UPDATE users SET id = 1"); err == nil ||
		!strings.Contains(err.Error(), "只支持 SELECT") {
		t.Errorf("update error = %v", err)
	}
}
