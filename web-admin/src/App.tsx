import { Navigate, Route, Routes } from 'react-router'
import RequireAuth from './auth/RequireAuth'
import AppLayout from './layout/AppLayout'
import AlertConfigPage from './pages/alerts/config'
import AlertLogsPage from './pages/alerts/logs'
import CollectTasksPage from './pages/collect-tasks'
import DataSourceDetailPage from './pages/data-sources/detail'
import DataSourcesPage from './pages/data-sources'
import EndpointsPage from './pages/endpoints'
import LoginPage from './pages/login'
import MonitorPage from './pages/monitor'
import OverviewPage from './pages/overview'
import PipelineRunDetailPage from './pages/pipeline-runs/detail'
import PipelineDetailPage from './pages/pipelines/detail'
import PipelinesPage from './pages/pipelines'
import QueryTemplatesPage from './pages/query-templates'
import QueryTemplateWizardPage from './pages/query-templates/wizard'
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
        <Route path="sources" element={<EndpointsPage />} />
        <Route path="pipelines" element={<PipelinesPage />} />
        <Route path="pipelines/:id" element={<PipelineDetailPage />} />
        <Route path="pipeline-runs/:id" element={<PipelineRunDetailPage />} />
        <Route path="datasets" element={<QueryTemplatesPage />} />
        <Route path="datasets/new" element={<QueryTemplateWizardPage />} />
        <Route path="datasets/:id/edit" element={<QueryTemplateWizardPage />} />
        {/* 兼容改名前的旧链接 */}
        <Route path="query-templates/*" element={<Navigate to="/datasets" replace />} />
        <Route path="collect-tasks" element={<CollectTasksPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="runs/:id" element={<RunDetailPage />} />
        <Route path="monitor" element={<MonitorPage />} />
        <Route path="alerts/config" element={<AlertConfigPage />} />
        <Route path="alerts/logs" element={<AlertLogsPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
