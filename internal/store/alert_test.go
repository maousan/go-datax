package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAlertLogCreateListAndDelete(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "alert.db"))
	now := time.Now()

	entries := []AlertLog{
		{
			Event:       "failed",
			Status:      AlertStatusFailed,
			ChannelName: "ops-mail",
			ChannelType: "email",
			SyncJobName: "nightly-sync",
			Error:       "smtp down",
			CreatedAt:   now.Add(-2 * time.Hour),
		},
		{
			Event:       "test",
			Status:      AlertStatusSuccess,
			ChannelName: "ops-webhook",
			ChannelType: "webhook",
			CreatedAt:   now.Add(-time.Hour),
		},
		{
			Event:       "failed",
			Status:      AlertStatusSuccess,
			ChannelName: "ops-webhook",
			ChannelType: "webhook",
			SyncJobName: "hourly-sync",
			CreatedAt:   now,
		},
	}
	for i := range entries {
		if err := s.CreateAlertLog(&entries[i]); err != nil {
			t.Fatalf("CreateAlertLog() failed: %v", err)
		}
	}

	logs, total, err := s.ListAlertLogs(AlertLogFilter{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListAlertLogs() failed: %v", err)
	}
	if total != 3 || len(logs) != 2 {
		t.Fatalf("page 1 = %d/%d, want 2/3", len(logs), total)
	}
	if logs[0].ID != entries[2].ID || logs[1].ID != entries[1].ID {
		t.Errorf("page 1 order = %d,%d, want newest first", logs[0].ID, logs[1].ID)
	}

	page2, total, err := s.ListAlertLogs(AlertLogFilter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("ListAlertLogs(page 2) failed: %v", err)
	}
	if total != 3 || len(page2) != 1 || page2[0].ID != entries[0].ID {
		t.Errorf("page 2 = %+v (total %d)", page2, total)
	}

	tests := []struct {
		name   string
		filter AlertLogFilter
		want   int64
	}{
		{name: "event", filter: AlertLogFilter{Event: "failed"}, want: 2},
		{name: "status", filter: AlertLogFilter{Status: AlertStatusFailed}, want: 1},
		{name: "channel type", filter: AlertLogFilter{ChannelType: "webhook"}, want: 2},
		{name: "query", filter: AlertLogFilter{Query: "hourly"}, want: 1},
		// 关键字与事件必须同时满足，验证 OR 条件被正确分组。
		{name: "query and event", filter: AlertLogFilter{Query: "ops", Event: "test"}, want: 1},
		{name: "from", filter: AlertLogFilter{From: ptrTime(now.Add(-90 * time.Minute))}, want: 2},
		{name: "to", filter: AlertLogFilter{To: ptrTime(now.Add(-90 * time.Minute))}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, total, err := s.ListAlertLogs(tt.filter)
			if err != nil {
				t.Fatalf("ListAlertLogs() failed: %v", err)
			}
			if total != tt.want || int64(len(logs)) != tt.want {
				t.Errorf("total = %d, items = %d, want %d", total, len(logs), tt.want)
			}
		})
	}

	cutoff := now.Add(-30 * time.Minute)
	deleted, err := s.DeleteAlertLogs(&cutoff)
	if err != nil {
		t.Fatalf("DeleteAlertLogs(before) failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if _, total, _ := s.ListAlertLogs(AlertLogFilter{}); total != 1 {
		t.Errorf("remaining = %d, want 1", total)
	}

	deleted, err = s.DeleteAlertLogs(nil)
	if err != nil {
		t.Fatalf("DeleteAlertLogs(all) failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted all = %d, want 1", deleted)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

func TestAlertLogSourceFilterAndPipelineFields(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "alert-source.db"))
	pipelineID := int64(11)
	pipelineRunID := int64(22)
	entries := []AlertLog{
		{SourceKind: AlertSourceDataX, Event: "failed", Status: AlertStatusSuccess, SyncJobName: "datax-job", ChannelName: "ops"},
		{SourceKind: "", Event: "failed", Status: AlertStatusSuccess, SyncJobName: "legacy-job", ChannelName: "ops"},
		{
			SourceKind: AlertSourcePipeline, Event: "failed", Status: AlertStatusSuccess,
			ChannelName: "ops", PipelineID: &pipelineID, PipelineName: "etl-job",
			PipelineRunID: &pipelineRunID, VersionNumber: 3,
		},
	}
	for i := range entries {
		if err := s.CreateAlertLog(&entries[i]); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a row created before source_kind existed.
	if err := s.db.Exec("UPDATE alert_logs SET source_kind = '' WHERE sync_job_name = 'legacy-job'").Error; err != nil {
		t.Fatal(err)
	}

	logs, total, err := s.ListAlertLogs(AlertLogFilter{SourceKind: AlertSourceDataX})
	if err != nil || total != 2 || len(logs) != 2 {
		t.Fatalf("datax logs = %+v, total %d, err %v", logs, total, err)
	}
	logs, total, err = s.ListAlertLogs(AlertLogFilter{SourceKind: AlertSourcePipeline})
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("pipeline logs = %+v, total %d, err %v", logs, total, err)
	}
	if logs[0].PipelineID == nil || *logs[0].PipelineID != pipelineID ||
		logs[0].PipelineRunID == nil || *logs[0].PipelineRunID != pipelineRunID ||
		logs[0].VersionNumber != 3 {
		t.Fatalf("pipeline fields = %+v", logs[0])
	}
	if _, total, err = s.ListAlertLogs(AlertLogFilter{Query: "etl-job"}); err != nil || total != 1 {
		t.Fatalf("pipeline keyword total = %d, err %v", total, err)
	}
}
