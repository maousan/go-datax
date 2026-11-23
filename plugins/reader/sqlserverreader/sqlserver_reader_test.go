package sqlserverreader

import (
	"testing"

	"github.com/longkeyy/go-datax/common/element"
)

func TestConvertToColumnHandlesDecimalBytes(t *testing.T) {
	task := NewSQLServerReaderTask()

	cases := []struct {
		name     string
		value    interface{}
		wantType element.ColumnType
		check    func(element.Column) bool
	}{
		{
			name:     "decimal bytes become double",
			value:    []byte("12.3400"),
			wantType: element.TypeDouble,
			check: func(column element.Column) bool {
				value, err := column.GetAsDouble()
				return err == nil && value == 12.34
			},
		},
		{
			name:     "integer bytes become long",
			value:    []byte("42"),
			wantType: element.TypeLong,
			check: func(column element.Column) bool {
				value, err := column.GetAsLong()
				return err == nil && value == 42
			},
		},
		{
			name:     "text bytes become string",
			value:    []byte("1PFXS202611230001"),
			wantType: element.TypeString,
			check: func(column element.Column) bool {
				return column.GetAsString() == "1PFXS202611230001"
			},
		},
		{
			name:     "nil becomes null",
			value:    nil,
			wantType: element.TypeString,
			check: func(column element.Column) bool {
				return column.IsNull()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			column := task.convertToColumn(tc.value)
			if column.GetType() != tc.wantType {
				t.Errorf("type = %v, want %v", column.GetType(), tc.wantType)
			}
			if !tc.check(column) {
				t.Errorf("value check failed for %v", tc.value)
			}
		})
	}
}

func TestConvertToColumnKeepsBinaryForNonPrintableBytes(t *testing.T) {
	task := NewSQLServerReaderTask()
	column := task.convertToColumn([]byte{0x00, 0x01, 0x02})
	if column.GetType() != element.TypeBytes {
		t.Errorf("type = %v, want TypeBytes", column.GetType())
	}
}
