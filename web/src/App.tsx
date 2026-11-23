import { Navigate, Route, Routes } from 'react-router'
import AppLayout from './layout/AppLayout'
import DataSourcesPage from './pages/DataSourcesPage'
import OverviewPage from './pages/OverviewPage'
import RunsPage from './pages/RunsPage'
import SyncJobsPage from './pages/SyncJobsPage'

export default function App() {
  return (
    <Routes>
      <Route element={<AppLayout />}>
        <Route index element={<OverviewPage />} />
        <Route path="data-sources" element={<DataSourcesPage />} />
        <Route path="sync-jobs" element={<SyncJobsPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
