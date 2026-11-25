package transform

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/expr-lang/expr"
)

// libraryOptions returns the fixed, versioned expression function library.
// Version 1 covers string, date, numeric, digest and masking helpers.
func libraryOptions() []expr.Option {
	return []expr.Option{
		expr.Function("substr", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return substr(params[0].(string), asInt64(params[1]), asInt64(params[2])), nil
		}, new(func(string, int, int) string), new(func(string, int64, int64) string)),
		expr.Function("to_int", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return toInt64(params[0])
		}, new(func(any) int64)),
		expr.Function("to_float", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return toFloat64(params[0])
		}, new(func(any) float64)),
		expr.Function("to_string", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return toString(params[0]), nil
		}, new(func(any) string)),
		expr.Function("to_bool", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return toBool(params[0])
		}, new(func(any) bool)),
		expr.Function("to_time", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return parseTimeString(params[0].(string), "")
		}, new(func(string) time.Time)),
		expr.Function("parse_time", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return parseTimeString(params[0].(string), params[1].(string))
		}, new(func(string, string) time.Time)),
		expr.Function("format_time", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			layout := params[1].(string)
			if layout == "" {
				layout = time.RFC3339Nano
			}
			return params[0].(time.Time).Format(layout), nil
		}, new(func(time.Time, string) string)),
		expr.Function("date_add", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			duration, err := time.ParseDuration(params[1].(string))
			if err != nil {
				return nil, err
			}
			return params[0].(time.Time).Add(duration), nil
		}, new(func(time.Time, string) time.Time)),
		expr.Function("round_to", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			scale := math.Pow10(int(asInt64(params[1])))
			return math.Round(params[0].(float64)*scale) / scale, nil
		}, new(func(float64, int) float64), new(func(float64, int64) float64)),
		expr.Function("md5", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			sum := md5.Sum([]byte(params[0].(string)))
			return hex.EncodeToString(sum[:]), nil
		}, new(func(string) string)),
		expr.Function("sha1", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			sum := sha1.Sum([]byte(params[0].(string)))
			return hex.EncodeToString(sum[:]), nil
		}, new(func(string) string)),
		expr.Function("sha256", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			sum := sha256.Sum256([]byte(params[0].(string)))
			return hex.EncodeToString(sum[:]), nil
		}, new(func(string) string)),
		expr.Function("mask_email", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return maskEmail(params[0].(string)), nil
		}, new(func(string) string)),
		expr.Function("mask_phone", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return maskPhone(params[0].(string)), nil
		}, new(func(string) string)),
		expr.Function("mask_id", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return maskID(params[0].(string), asInt64(params[1]), asInt64(params[2])), nil
		}, new(func(string, int, int) string), new(func(string, int64, int64) string)),
		expr.Function("mask_all", func(params ...any) (any, error) {
			if hasNull(params...) {
				return nil, nil
			}
			return strings.Repeat("*", utf8.RuneCountInString(params[0].(string))), nil
		}, new(func(string) string)),
		expr.Function("is_null", func(params ...any) (any, error) {
			return params[0] == nil, nil
		}, new(func(any) bool)),
	}
}

func hasNull(params ...any) bool {
	for _, param := range params {
		if param == nil {
			return true
		}
	}
	return false
}

func substr(value string, start, length int64) string {
	runes := []rune(value)
	if start < 0 || length < 0 || start >= int64(len(runes)) {
		return ""
	}
	end := start + length
	if end > int64(len(runes)) {
		end = int64(len(runes))
	}
	return string(runes[start:end])
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		return 0
	}
}

func toInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if typed != math.Trunc(typed) {
			return 0, fmt.Errorf("value %v is not an integer", typed)
		}
		return int64(typed), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", value)
	}
}

func toFloat64(value any) (float64, error) {
	switch typed := value.(type) {
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case float64:
		return typed, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}

func toString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case time.Time:
		return typed.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value)
	}
}

func toBool(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		return strconv.ParseBool(strings.TrimSpace(typed))
	case int64:
		return typed != 0, nil
	case float64:
		return typed != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", value)
	}
}

func parseTimeString(value, layout string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if layout != "" {
		return time.Parse(layout, value)
	}
	for _, candidate := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(candidate, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", value)
}

func maskEmail(value string) string {
	at := strings.LastIndex(value, "@")
	if at <= 0 {
		return maskID(value, 0, 0)
	}
	local := []rune(value[:at])
	domain := value[at:]
	if len(local) <= 1 {
		return "*" + domain
	}
	return string(local[0]) + strings.Repeat("*", len(local)-1) + domain
}

func maskPhone(value string) string {
	runes := []rune(value)
	if len(runes) < 7 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:3]) + strings.Repeat("*", len(runes)-7) + string(runes[len(runes)-4:])
}

func maskID(value string, left, right int64) string {
	runes := []rune(value)
	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}
	if left+right >= int64(len(runes)) {
		return strings.Repeat("*", len(runes))
	}
	prefix := string(runes[:left])
	suffix := ""
	if right > 0 {
		suffix = string(runes[int64(len(runes))-right:])
	}
	return prefix + strings.Repeat("*", len(runes)-int(left+right)) + suffix
}
