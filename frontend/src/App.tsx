import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from './components/AppShell';
import { AdminContainersPage } from './pages/AdminContainersPage';
import { AdminContainerTypesPage } from './pages/AdminContainerTypesPage';
import { AdminAIPage } from './pages/AdminAIPage';
import { AdminBackupPage } from './pages/AdminBackupPage';
import { AdminInventoryGroupsPage } from './pages/AdminInventoryGroupsPage';
import { AdminMetricsPage } from './pages/AdminMetricsPage';
import { AdminLocationsPage } from './pages/AdminLocationsPage';
import { AdminPage } from './pages/AdminPage';
import { AdminSellPage } from './pages/AdminSellPage';
import { AdminSystemPage } from './pages/AdminSystemPage';
import { AdminStubPage } from './pages/AdminStubPage';
import { InventoryPage } from './pages/InventoryPage';
import { ReviewPage } from './pages/ReviewPage';
import { UploadPage } from './pages/UploadPage';
import { WholeScenePage } from './pages/WholeScenePage';

export function App() {
  return (
    <AppShell>
      <Routes>
        <Route path="/" element={<Navigate to="/upload" replace />} />
        <Route path="/upload" element={<UploadPage />} />
        <Route path="/admin" element={<AdminPage />} />
        <Route path="/admin/inventory-groups" element={<AdminInventoryGroupsPage />} />
        <Route path="/admin/container-types" element={<AdminContainerTypesPage />} />
        <Route path="/admin/locations" element={<AdminLocationsPage />} />
        <Route path="/admin/containers" element={<AdminContainersPage />} />
        <Route path="/admin/backup-restore" element={<AdminBackupPage />} />
        <Route
          path="/admin/uploads"
          element={
            <AdminStubPage
              title="Upload Administration"
              description="This page will later inspect upload sessions, failures, and processing state."
            />
          }
        />
        <Route
          path="/admin/system"
          element={<AdminSystemPage />}
        />
        <Route
          path="/admin/ai"
          element={<AdminAIPage />}
        />
        <Route path="/admin/sell" element={<AdminSellPage />} />
        <Route path="/admin/metrics" element={<AdminMetricsPage />} />
        <Route path="/inventory" element={<InventoryPage />} />
        <Route path="/review" element={<ReviewPage />} />
        <Route path="/wholescene" element={<WholeScenePage />} />
        <Route path="*" element={<Navigate to="/upload" replace />} />
      </Routes>
    </AppShell>
  );
}
