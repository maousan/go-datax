package oraclewriter

import (
	"strings"
	"testing"
)

func TestBuildUpdateStatement(t *testing.T) {
	columns := []string{"BILLNO", "REFBILLNO", "PLUCODE", "RECCOUNT"}
	statement, argsOrder, err := buildUpdateStatement(
		"twmsio_wslth_ok_v2",
		columns,
		[]string{"BILLNO", "PLUCODE"},
	)
	if err != nil {
		t.Fatalf("buildUpdateStatement() failed: %v", err)
	}

	want := "UPDATE twmsio_wslth_ok_v2 SET REFBILLNO = :col2, RECCOUNT = :col4 WHERE BILLNO = :col1 AND PLUCODE = :col3"
	if statement != want {
		t.Errorf("statement = %q, want %q", statement, want)
	}

	wantOrder := []int{1, 3, 0, 2}
	if len(argsOrder) != len(wantOrder) {
		t.Fatalf("args order = %v, want %v", argsOrder, wantOrder)
	}
	for i := range wantOrder {
		if argsOrder[i] != wantOrder[i] {
			t.Errorf("args order = %v, want %v", argsOrder, wantOrder)
			break
		}
	}
}

func TestBuildUpdateStatementMatchesKeysCaseInsensitively(t *testing.T) {
	columns := []string{"BILLNO", "REFBILLNO", "PLUCODE"}
	statement, argsOrder, err := buildUpdateStatement("t", columns, []string{"billno", "plucode"})
	if err != nil {
		t.Fatalf("buildUpdateStatement() failed: %v", err)
	}
	if !strings.Contains(statement, "WHERE BILLNO = :col1 AND PLUCODE = :col3") {
		t.Errorf("unexpected statement: %s", statement)
	}
	if len(argsOrder) != 3 || argsOrder[1] != 0 || argsOrder[2] != 2 {
		t.Errorf("args order = %v, want [1 0 2]", argsOrder)
	}
}

func TestBuildUpdateStatementRejectsMissingKey(t *testing.T) {
	if _, _, err := buildUpdateStatement("t", []string{"A", "B"}, []string{"MISSING"}); err == nil {
		t.Fatal("expected missing key to fail")
	}
}

func TestBuildUpdateStatementRejectsAllKeyColumns(t *testing.T) {
	if _, _, err := buildUpdateStatement("t", []string{"A", "B"}, []string{"A", "B"}); err == nil {
		t.Fatal("expected all-key columns to fail")
	}
}

func TestParseWriteModeUpdateKeys(t *testing.T) {
	job := &OracleWriterJob{columns: []string{"BILLNO", "PLUCODE", "RECCOUNT"}}
	if err := job.parseWriteMode("update(BILLNO, PLUCODE)"); err != nil {
		t.Fatalf("parseWriteMode() failed: %v", err)
	}
	if len(job.updateKeys) != 2 || job.updateKeys[0] != "BILLNO" || job.updateKeys[1] != "PLUCODE" {
		t.Errorf("updateKeys = %v", job.updateKeys)
	}

	if err := (&OracleWriterJob{columns: []string{"A"}}).parseWriteMode("UPDATE"); err == nil {
		t.Error("expected bare UPDATE to fail")
	}
	if err := (&OracleWriterJob{columns: []string{"A"}}).parseWriteMode("update(B)"); err == nil {
		t.Error("expected unknown key to fail")
	}
}
