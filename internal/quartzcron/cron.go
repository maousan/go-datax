// Package quartzcron implements the subset of Quartz cron expressions used by
// the admin scheduler: 6 or 7 fields (seconds, minutes, hours, day-of-month,
// month, day-of-week, optional year) with *, ?, lists, ranges, steps and
// month/weekday names. L, W and # are rejected explicitly.
package quartzcron

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var monthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
	"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var weekdayNames = map[string]int{
	"SUN": 1, "MON": 2, "TUE": 3, "WED": 4, "THU": 5, "FRI": 6, "SAT": 7,
}

// Schedule 是一个已解析的 Quartz cron 表达式。
type Schedule struct {
	seconds     []int
	minutes     []int
	hours       []int
	daysOfMonth []int
	months      []int
	daysOfWeek  []int // 0 = Sunday .. 6 = Saturday
	years       []int // nil = any
	domAny      bool
	dowAny      bool
}

// Parse 解析 Quartz cron 表达式。仅支持秒/分/时/日/月/周（可选年）六或七段。
func Parse(spec string) (*Schedule, error) {
	fields := strings.Fields(strings.TrimSpace(spec))
	if len(fields) != 6 && len(fields) != 7 {
		return nil, fmt.Errorf("cron 表达式需要 6 或 7 段（秒 分 时 日 月 周 [年]），当前 %d 段", len(fields))
	}

	schedule := &Schedule{}
	var err error
	if schedule.seconds, err = parseField(fields[0], 0, 59, nil, false); err != nil {
		return nil, fmt.Errorf("秒: %w", err)
	}
	if schedule.minutes, err = parseField(fields[1], 0, 59, nil, false); err != nil {
		return nil, fmt.Errorf("分: %w", err)
	}
	if schedule.hours, err = parseField(fields[2], 0, 23, nil, false); err != nil {
		return nil, fmt.Errorf("时: %w", err)
	}
	if schedule.daysOfMonth, schedule.domAny, err = parseFieldWithAny(fields[3], 1, 31, nil, true); err != nil {
		return nil, fmt.Errorf("日: %w", err)
	}
	if schedule.months, err = parseField(fields[4], 1, 12, monthNames, false); err != nil {
		return nil, fmt.Errorf("月: %w", err)
	}
	if schedule.daysOfWeek, schedule.dowAny, err = parseWeekField(fields[5]); err != nil {
		return nil, fmt.Errorf("周: %w", err)
	}
	if len(fields) == 7 && fields[6] != "*" {
		if schedule.years, err = parseField(fields[6], 1970, 2199, nil, false); err != nil {
			return nil, fmt.Errorf("年: %w", err)
		}
	}
	return schedule, nil
}

func parseField(raw string, min, max int, names map[string]int, allowQuestion bool) ([]int, error) {
	values, _, err := parseFieldWithAny(raw, min, max, names, allowQuestion)
	return values, err
}

func parseFieldWithAny(raw string, min, max int, names map[string]int, allowQuestion bool) ([]int, bool, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return nil, false, fmt.Errorf("不能为空")
	}
	if strings.ContainsAny(raw, "LW#") {
		return nil, false, fmt.Errorf("暂不支持 L/W/# 语法，请使用数字、范围或步长")
	}
	if strings.Contains(raw, "?") {
		if !allowQuestion {
			return nil, false, fmt.Errorf("? 只能用于日或周字段")
		}
		if raw != "?" {
			return nil, false, fmt.Errorf("? 不能与其他取值组合")
		}
		return fullRange(min, max), true, nil
	}

	any := raw == "*"
	allowed := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false, fmt.Errorf("存在空项")
		}
		values, err := parsePart(part, min, max, names)
		if err != nil {
			return nil, false, err
		}
		for _, value := range values {
			allowed[value] = true
		}
	}

	result := make([]int, 0, len(allowed))
	for value := range allowed {
		result = append(result, value)
	}
	sort.Ints(result)
	if len(result) == 0 {
		return nil, false, fmt.Errorf("没有有效取值")
	}
	return result, any, nil
}

func parsePart(part string, min, max int, names map[string]int) ([]int, error) {
	step := 1
	base := part
	if index := strings.Index(part, "/"); index >= 0 {
		if strings.Count(part, "/") > 1 {
			return nil, fmt.Errorf("步长格式不正确: %s", part)
		}
		base = strings.TrimSpace(part[:index])
		rawStep := strings.TrimSpace(part[index+1:])
		parsed, err := strconv.Atoi(rawStep)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("步长必须是正整数: %s", part)
		}
		step = parsed
	}

	start, end := min, max

	switch {
	case base == "*":
		// full range
	case strings.Contains(base, "-"):
		pieces := strings.Split(base, "-")
		if len(pieces) != 2 {
			return nil, fmt.Errorf("范围格式不正确: %s", base)
		}
		var err error
		start, err = parseValue(pieces[0], names)
		if err != nil {
			return nil, err
		}
		end, err = parseValue(pieces[1], names)
		if err != nil {
			return nil, err
		}
	default:
		value, err := parseValue(base, names)
		if err != nil {
			return nil, err
		}
		start, end = value, value
		if step > 1 {
			end = max
		}
	}

	if start < min || start > max || end < min || end > max {
		return nil, fmt.Errorf("取值超出范围 %d-%d: %s", min, max, part)
	}
	if start > end {
		return nil, fmt.Errorf("范围起点不能大于终点: %s", part)
	}
	var result []int
	for value := start; value <= end; value += step {
		result = append(result, value)
	}
	return result, nil
}

func parseWeekField(raw string) ([]int, bool, error) {
	values, any, err := parseFieldWithAny(raw, 1, 7, weekdayNames, true)
	if err != nil {
		return nil, false, err
	}
	result := make([]int, 0, len(values))
	for _, value := range values {
		// Quartz uses 1=Sunday .. 7=Saturday; Go uses 0=Sunday .. 6=Saturday.
		result = append(result, value-1)
	}
	sort.Ints(result)
	return result, any, nil
}

func parseValue(raw string, names map[string]int) (int, error) {
	raw = strings.TrimSpace(raw)
	if value, ok := names[raw]; ok {
		return value, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("无法识别的取值: %s", raw)
	}
	return value, nil
}

func fullRange(min, max int) []int {
	result := make([]int, 0, max-min+1)
	for value := min; value <= max; value++ {
		result = append(result, value)
	}
	return result
}

// Next 返回 after 之后的下一次执行时间；没有可执行时间时返回零值。
func (s *Schedule) Next(after time.Time) time.Time {
	start := after.Truncate(time.Second).Add(time.Second)
	location := start.Location()
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	limit := start.AddDate(8, 0, 0)

	for day.Before(limit) {
		if s.matchesDate(day) {
			if next, ok := s.firstOnDay(day, start); ok {
				return next
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}
}

func (s *Schedule) matchesDate(day time.Time) bool {
	if len(s.years) > 0 && !contains(s.years, day.Year()) {
		return false
	}
	if !contains(s.months, int(day.Month())) {
		return false
	}

	domMatch := contains(s.daysOfMonth, day.Day())
	dowMatch := contains(s.daysOfWeek, int(day.Weekday()))
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dowMatch
	case s.dowAny:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}

func (s *Schedule) firstOnDay(day, start time.Time) (time.Time, bool) {
	startHour, startMinute, startSecond := 0, 0, 0
	if sameDate(day, start) {
		startHour = start.Hour()
		startMinute = start.Minute()
		startSecond = start.Second()
	}

	for _, hour := range s.hours {
		if hour < startHour {
			continue
		}
		for _, minute := range s.minutes {
			if hour == startHour && minute < startMinute {
				continue
			}
			for _, second := range s.seconds {
				if hour == startHour && minute == startMinute && second < startSecond {
					continue
				}
				return time.Date(
					day.Year(), day.Month(), day.Day(),
					hour, minute, second, 0, day.Location(),
				), true
			}
		}
	}
	return time.Time{}, false
}

func sameDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func contains(values []int, want int) bool {
	index := sort.SearchInts(values, want)
	return index < len(values) && values[index] == want
}

// String 返回规范化后的表达式，便于日志排查。
func (s *Schedule) String() string {
	format := func(values []int) string {
		parts := make([]string, len(values))
		for index, value := range values {
			parts[index] = strconv.Itoa(value)
		}
		return strings.Join(parts, ",")
	}
	fields := []string{
		format(s.seconds), format(s.minutes), format(s.hours),
		format(s.daysOfMonth), format(s.months), format(s.daysOfWeek),
	}
	if len(s.years) > 0 {
		fields = append(fields, format(s.years))
	}
	return strings.Join(fields, " ")
}
