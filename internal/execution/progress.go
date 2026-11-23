package execution

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/longkeyy/go-datax/internal/store"
)

// progressPattern 匹配引擎输出的进度快照：
// Total 10 records, 1.00KB | Speed 512B/s, 10 records/s | Error 0 records, 0B | ... | Percentage 50.00%
var progressPattern = regexp.MustCompile(
	`Total (\d+) records, ([0-9.]+)(B|KB|MB|GB|TB|PB) \| Speed ([0-9.]+)(B|KB|MB|GB|TB|PB)/s, (\d+) records/s \| Error (\d+) records, [0-9.]+(?:B|KB|MB|GB|TB|PB).*?Percentage ([0-9.]+)%`,
)

func parseProgressLine(line string) (store.RunProgress, bool) {
	match := progressPattern.FindStringSubmatch(line)
	if match == nil {
		return store.RunProgress{}, false
	}

	readRecords, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return store.RunProgress{}, false
	}
	readBytes, err := parseProgressBytes(match[2], match[3])
	if err != nil {
		return store.RunProgress{}, false
	}
	speedBytes, err := parseProgressBytes(match[4], match[5])
	if err != nil {
		return store.RunProgress{}, false
	}
	speedRecords, err := strconv.ParseInt(match[6], 10, 64)
	if err != nil {
		return store.RunProgress{}, false
	}
	errorRecords, err := strconv.ParseInt(match[7], 10, 64)
	if err != nil {
		return store.RunProgress{}, false
	}
	percent, err := strconv.ParseFloat(match[8], 64)
	if err != nil {
		return store.RunProgress{}, false
	}

	return store.RunProgress{
		Percent:            percent,
		ReadRecords:        readRecords,
		ReadBytes:          readBytes,
		SpeedBytesPerSec:   speedBytes,
		SpeedRecordsPerSec: speedRecords,
		ErrorRecords:       errorRecords,
	}, true
}

func parseProgressBytes(value, unit string) (int64, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}

	multiplier := float64(1)
	switch strings.ToUpper(unit) {
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	case "TB":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "PB":
		multiplier = 1024 * 1024 * 1024 * 1024 * 1024
	}
	return int64(number * multiplier), nil
}
