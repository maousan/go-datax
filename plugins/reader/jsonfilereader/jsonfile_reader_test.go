package jsonfilereader

import (
	"testing"

	"github.com/longkeyy/go-datax/common/factory"
)

// parseOrderedJSON 必须保留对象字段的原始顺序（通配符模式依赖该顺序）
func TestParseOrderedJSONPreservesKeyOrder(t *testing.T) {
	parsed, err := parseOrderedJSON([]byte(`{"z":1,"a":2,"m":3}`))
	if err != nil {
		t.Fatalf("parseOrderedJSON failed: %v", err)
	}

	obj, ok := parsed.(*orderedObject)
	if !ok {
		t.Fatalf("expected *orderedObject, got %T", parsed)
	}

	want := []string{"z", "a", "m"}
	if len(obj.keys) != len(want) {
		t.Fatalf("expected %d keys, got %d: %v", len(want), len(obj.keys), obj.keys)
	}
	for i, key := range want {
		if obj.keys[i] != key {
			t.Errorf("key[%d] = %q, want %q (order must match source)", i, obj.keys[i], key)
		}
	}
}

func TestParseOrderedJSONRejectsInvalidInput(t *testing.T) {
	cases := []string{
		`{"a":1`,
		`{"a":1} trailing`,
		`[1,2`,
	}
	for _, input := range cases {
		if _, err := parseOrderedJSON([]byte(input)); err == nil {
			t.Errorf("expected error for input %q, got nil", input)
		}
	}
}

// 通配符模式：按源字段顺序输出，并保留原始类型
func TestWildcardRecordOrderAndTypes(t *testing.T) {
	task := &JSONFileReaderTask{
		readerJob: &JSONFileReaderJob{
			columns: []map[string]interface{}{{"type": "string", "wildcard": true}},
		},
		factory: factory.GetGlobalFactory(),
	}

	parsed, err := parseOrderedJSON([]byte(`{"b":2,"a":"x","c":true,"d":1.5,"e":{"n":1},"f":[1,2]}`))
	if err != nil {
		t.Fatalf("parseOrderedJSON failed: %v", err)
	}

	record, err := task.convertJSONToRecord(parsed)
	if err != nil {
		t.Fatalf("convertJSONToRecord failed: %v", err)
	}

	if got := record.GetColumnNumber(); got != 6 {
		t.Fatalf("expected 6 columns, got %d", got)
	}

	if v, err := record.GetColumn(0).GetAsLong(); err != nil || v != 2 {
		t.Errorf("column[0] (b) = %v, %v; want long 2", v, err)
	}
	if got := record.GetColumn(1).GetAsString(); got != "x" {
		t.Errorf("column[1] (a) = %q, want \"x\"", got)
	}
	if v, err := record.GetColumn(2).GetAsBool(); err != nil || !v {
		t.Errorf("column[2] (c) = %v, %v; want bool true", v, err)
	}
	if v, err := record.GetColumn(3).GetAsDouble(); err != nil || v != 1.5 {
		t.Errorf("column[3] (d) = %v, %v; want double 1.5", v, err)
	}
	if got := record.GetColumn(4).GetAsString(); got != `{"n":1}` {
		t.Errorf("column[4] (e) = %q, want nested JSON {\"n\":1}", got)
	}
	if got := record.GetColumn(5).GetAsString(); got != `[1,2]` {
		t.Errorf("column[5] (f) = %q, want [1,2]", got)
	}
}

// 点号嵌套字段访问需要支持有序对象
func TestNestedFieldAccessWithOrderedObject(t *testing.T) {
	task := &JSONFileReaderTask{
		readerJob: &JSONFileReaderJob{
			columns: []map[string]interface{}{
				{"name": "user.name", "type": "string"},
				{"name": "user.age", "type": "long"},
			},
		},
		factory: factory.GetGlobalFactory(),
	}

	parsed, err := parseOrderedJSON([]byte(`{"user":{"age":33,"name":"Nested"}}`))
	if err != nil {
		t.Fatalf("parseOrderedJSON failed: %v", err)
	}

	record, err := task.convertJSONToRecord(parsed)
	if err != nil {
		t.Fatalf("convertJSONToRecord failed: %v", err)
	}

	if got := record.GetColumn(0).GetAsString(); got != "Nested" {
		t.Errorf("user.name = %q, want \"Nested\"", got)
	}
	if v, err := record.GetColumn(1).GetAsLong(); err != nil || v != 33 {
		t.Errorf("user.age = %v, %v; want 33", v, err)
	}
}
