package server

import "net/http"

// handleMonitor 返回进程运行时指标快照（CPU、协程、内存等）。
func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.monitor.Snapshot())
}
