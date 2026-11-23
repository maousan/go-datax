package execution

import "testing"

func TestParseProgressLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantOK     bool
		wantRecord int64
		wantBytes  int64
		wantSpeed  int64
		wantSpeedR int64
		wantErrors int64
		wantPct    float64
	}{
		{
			name:       "full snapshot with log prefix",
			line:       "2026-11-23T16:58:13.728+0800\tINFO\tAPP\tTotal 2000 records, 1.95MB | Speed 70.31KB/s, 2000 records/s | Error 3 records, 12B | All Task WaitWriterTime 0ms | All Task WaitReaderTime 0ms | Percentage 100.00%",
			wantOK:     true,
			wantRecord: 2000,
			wantBytes:  2044723,
			wantSpeed:  71997,
			wantSpeedR: 2000,
			wantErrors: 3,
			wantPct:    100,
		},
		{
			name:       "transformer segment",
			line:       "Total 10 records, 1.00KB | Speed 0B/s, 0 records/s | Error 0 records, 0B | All Task WaitWriterTime 1ms | All Task WaitReaderTime 2ms | Transformer Success 1 records | Transformer Error 0 records | Transformer Filter 0 records | Transformer usedTime 0ms | Percentage 50.00%",
			wantOK:     true,
			wantRecord: 10,
			wantBytes:  1024,
			wantSpeed:  0,
			wantSpeedR: 0,
			wantErrors: 0,
			wantPct:    50,
		},
		{
			name:       "empty progress",
			line:       "Total 0 records, 0B | Speed 0B/s, 0 records/s | Error 0 records, 0B | All Task WaitWriterTime 0ms | All Task WaitReaderTime 0ms | Percentage 0.00%",
			wantOK:     true,
			wantRecord: 0,
			wantBytes:  0,
			wantSpeed:  0,
			wantSpeedR: 0,
			wantErrors: 0,
			wantPct:    0,
		},
		{
			name:   "unrelated line",
			line:   "2026-11-23T16:58:13.728+0800\tINFO\tAPP\tJob completed",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			progress, ok := parseProgressLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if progress.ReadRecords != tt.wantRecord {
				t.Errorf("ReadRecords = %d, want %d", progress.ReadRecords, tt.wantRecord)
			}
			if progress.ReadBytes != tt.wantBytes {
				t.Errorf("ReadBytes = %d, want %d", progress.ReadBytes, tt.wantBytes)
			}
			if progress.SpeedBytesPerSec != tt.wantSpeed {
				t.Errorf("SpeedBytesPerSec = %d, want %d", progress.SpeedBytesPerSec, tt.wantSpeed)
			}
			if progress.SpeedRecordsPerSec != tt.wantSpeedR {
				t.Errorf("SpeedRecordsPerSec = %d, want %d", progress.SpeedRecordsPerSec, tt.wantSpeedR)
			}
			if progress.ErrorRecords != tt.wantErrors {
				t.Errorf("ErrorRecords = %d, want %d", progress.ErrorRecords, tt.wantErrors)
			}
			if progress.Percent != tt.wantPct {
				t.Errorf("Percent = %v, want %v", progress.Percent, tt.wantPct)
			}
		})
	}
}
