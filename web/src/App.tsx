import { Navigate, Route, Routes } from 'react-router'
import RequireAuth from './auth/RequireAuth'
import AppLayout from './layout/AppLayout'
import DataSourcesPage from './pages/DataSourcesPage'
import LoginPage from './pages/LoginPage'
import OverviewPage from './pages/OverviewPage'
import RunsPage from './pages/RunsPage'
import RunDetailPage from './pages/RunDetailPage'
import SyncJobsPage from './pages/SyncJobsPage'

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
        <Route path="sync-jobs" element={<SyncJobsPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="runs/:id" element={<RunDetailPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
