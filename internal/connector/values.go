package connector

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func coerceValue(value any, dataType DataType) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch dataType {
	case TypeInt64:
		switch typed := value.(type) {
		case int64:
			return typed, nil
		case int:
			return int64(typed), nil
		case float64:
			if typed != float64(int64(typed)) {
				return nil, fmt.Errorf("cannot convert %v to int64", typed)
			}
			return int64(typed), nil
		case []byte:
			return strconv.ParseInt(string(typed), 10, 64)
		case string:
			return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		default:
			return nil, fmt.Errorf("cannot convert %T to int64", value)
		}
	case TypeFloat64:
		switch typed := value.(type) {
		case float64:
			return typed, nil
		case int64:
			return float64(typed), nil
		case []byte:
			return strconv.ParseFloat(string(typed), 64)
		case string:
			return strconv.ParseFloat(strings.TrimSpace(typed), 64)
		default:
			return nil, fmt.Errorf("cannot convert %T to float64", value)
		}
	case TypeDecimal:
		switch typed := value.(type) {
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64), nil
		case []byte:
			return string(typed), nil
		case string:
			return typed, nil
		default:
			return fmt.Sprint(value), nil
		}
	case TypeString:
		switch typed := value.(type) {
		case string:
			return typed, nil
		case []byte:
			return string(typed), nil
		case time.Time:
			return typed.Format(time.RFC3339Nano), nil
		default:
			return fmt.Sprint(value), nil
		}
	case TypeBytes:
		switch typed := value.(type) {
		case []byte:
			return typed, nil
		case string:
			return []byte(typed), nil
		default:
			return nil, fmt.Errorf("cannot convert %T to bytes", value)
		}
	case TypeBool:
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case int64:
			return typed != 0, nil
		case []byte:
			return strconv.ParseBool(string(typed))
		case string:
			return strconv.ParseBool(strings.TrimSpace(typed))
		default:
			return nil, fmt.Errorf("cannot convert %T to bool", value)
		}
	case TypeTimestamp:
		switch typed := value.(type) {
		case time.Time:
			return typed, nil
		case []byte:
			return parseTimestamp(string(typed))
		case string:
			return parseTimestamp(typed)
		default:
			return nil, fmt.Errorf("cannot convert %T to timestamp", value)
		}
	default:
		return nil, fmt.Errorf("unsupported type %q", dataType)
	}
}

func parseTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q", raw)
}

func schemaEqual(left, right Schema) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !strings.EqualFold(left[index].Name, right[index].Name) || left[index].Type != right[index].Type {
			return false
		}
	}
	return true
}
