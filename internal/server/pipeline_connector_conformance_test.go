package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/longkeyy/go-datax/internal/connectiontest"
)

// These tests exercise the full Pipeline path (management API → worker →
// MySQL/PostgreSQL). They are skipped unless the matching JDBC URL is set.
func TestPipelineDatabaseConnectorConformance(t *testing.T) {
	type testCase struct {
		name        string
		dsType      string
		urlEnv      string
		userEnv     string
		passEnv     string
		createSQL   func(string) string
		placeholder func(int) string
	}
	cases := []testCase{
		{
			name:    "mysql",
			dsType:  "mysql",
			urlEnv:  "ETL_MYSQL_JDBC_URL",
			userEnv: "ETL_MYSQL_USER",
			passEnv: "ETL_MYSQL_PASSWORD",
			createSQL: func(table string) string {
				return fmt.Sprintf(`CREATE TABLE %s (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(100) NOT NULL,
  amount DECIMAL(18,4) NOT NULL
)`, table)
			},
			placeholder: func(int) string { return "?" },
		},
		{
			name:    "postgres",
			dsType:  "postgres",
			urlEnv:  "ETL_POSTGRES_JDBC_URL",
			userEnv: "ETL_POSTGRES_USER",
			passEnv: "ETL_POSTGRES_PASSWORD",
			createSQL: func(table string) string {
				return fmt.Sprintf(`CREATE TABLE %s (
  id BIGINT NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  amount NUMERIC(18,4) NOT NULL
)`, table)
			},
			placeholder: func(i int) string { return fmt.Sprintf("$%d", i) },
		},
	}

	var active []testCase
	for _, tc := range cases {
		if os.Getenv(tc.urlEnv) != "" {
			active = append(active, tc)
		}
	}
	if len(active) == 0 {
		t.Skip("ETL_MYSQL_JDBC_URL / ETL_POSTGRES_JDBC_URL not set")
	}

	_, handler, _ := newPipelineTestServer(t)
	token := loginToken(t, handler, "admin", "secret")

	for _, tc := range active {
		t.Run(tc.name, func(t *testing.T) {
			params := connectiontest.Params{
				Type:     tc.dsType,
				JDBCUrl:  os.Getenv(tc.urlEnv),
				Username: os.Getenv(tc.userEnv),
				Password: os.Getenv(tc.passEnv),
			}
			db, _, err := connectiontest.Open(params)
			if err != nil {
				t.Fatalf("open %s: %v", tc.name, err)
			}
			defer db.Close()

			suffix := time.Now().UnixNano()
			sourceTable := fmt.Sprintf("etl_pipe_src_%d", suffix)
			targetTable := fmt.Sprintf("etl_pipe_dst_%d", suffix)
			for _, table := range []string{sourceTable, targetTable} {
				if _, err := db.Exec(tc.createSQL(table)); err != nil {
					t.Fatalf("create %s: %v", table, err)
				}
				table := table
				defer func() { _, _ = db.Exec("DROP TABLE " + table) }()
			}
			insertPipeRows(t, db, tc.placeholder, sourceTable, [][]any{
				{int64(1), "one", "1.1000"},
				{int64(2), "two", "2.2000"},
				{int64(3), "three", "3.3000"},
			})
			insertPipeRows(t, db, tc.placeholder, targetTable, [][]any{
				{int64(2), "stale", "0.0000"},
			})

			ds := createDataSource(t, handler, token, tc.name+"-pipeline", tc.dsType,
				params.JDBCUrl, params.Username, params.Password)
			schema := []map[string]string{
				{"name": "id", "type": "int64"},
				{"name": "name", "type": "string"},
				{"name": "amount", "type": "decimal"},
			}
			sourceBody := endpointBody(tc.name+"-source", tc.dsType, ds.ID, schema)
			sourceBody["table"] = sourceTable
			sourceBody["options"] = map[string]string{"splitPk": "id"}
			source := createEndpoint(t, handler, token, "sources", sourceBody)

			sinkBody := endpointBody(tc.name+"-sink", tc.dsType, ds.ID, schema)
			sinkBody["table"] = targetTable
			sinkBody["options"] = map[string]string{"writeMode": "upsert"}
			sink := createEndpoint(t, handler, token, "sinks", sinkBody)

			rec := authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines", token, map[string]any{
				"name":       tc.name + "-pipeline-" + fmt.Sprint(suffix),
				"sourceId":   source.ID,
				"sinkId":     sink.ID,
				"transforms": []any{},
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("create pipeline status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var pipeline pipelineResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
				t.Fatal(err)
			}
			rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/publish", token, nil)
			if rec.Code != http.StatusCreated {
				t.Fatalf("publish status = %d, body = %s", rec.Code, rec.Body.String())
			}
			rec = authedJSONRequest(t, handler, http.MethodPost, "/api/v1/pipelines/"+itoa(pipeline.ID)+"/runs", token, nil)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("run status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var run pipelineRunResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
				t.Fatal(err)
			}
			run = waitPipelineRun(t, handler, token, run.ID)
			if run.Status != "succeeded" || run.ReadRecords != 3 || run.WriteRecords != 3 {
				t.Fatalf("run = %+v", run)
			}

			rows, err := db.Query("SELECT id, name, amount FROM " + targetTable + " ORDER BY id")
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			defer rows.Close()
			var got []string
			for rows.Next() {
				var id int64
				var name, amount string
				if err := rows.Scan(&id, &name, &amount); err != nil {
					t.Fatalf("scan target: %v", err)
				}
				got = append(got, fmt.Sprintf("%d:%s:%s", id, name, amount))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != "1:one:1.1000,2:two:2.2000,3:three:3.3000" {
				t.Fatalf("target rows = %v", got)
			}
		})
	}
}

func insertPipeRows(t *testing.T, db *sql.DB, placeholder func(int) string, table string, rows [][]any) {
	t.Helper()
	statement := "INSERT INTO " + table + " (id, name, amount) VALUES (" +
		placeholder(1) + ", " + placeholder(2) + ", " + placeholder(3) + ")"
	for index, row := range rows {
		if _, err := db.Exec(statement, row...); err != nil {
			t.Fatalf("insert row %d: %v", index, err)
		}
	}
}
