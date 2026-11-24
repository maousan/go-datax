import { Navigate, Route, Routes } from 'react-router'
import RequireAuth from './auth/RequireAuth'
import AppLayout from './layout/AppLayout'
import DataSourceDetailPage from './pages/data-sources/detail'
import DataSourcesPage from './pages/data-sources'
import LoginPage from './pages/login'
import MonitorPage from './pages/monitor'
import OverviewPage from './pages/overview'
import RunDetailPage from './pages/runs/detail'
import RunsPage from './pages/runs'
import SettingsPage from './pages/settings'
import SyncJobDetailPage from './pages/sync-jobs/detail'
import SyncJobsPage from './pages/sync-jobs'

export default function App() {
  return (
    <Routes>
      <Route path="login" element={<LoginPage />} />

      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route index element={<OverviewPage />} />
        <Route path="data-sources" element={<DataSourcesPage />} />
        <Route path="data-sources/:id" element={<DataSourceDetailPage />} />
        <Route path="sync-jobs" element={<SyncJobsPage />} />
        <Route path="sync-jobs/:id" element={<SyncJobDetailPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="runs/:id" element={<RunDetailPage />} />
        <Route path="monitor" element={<MonitorPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
