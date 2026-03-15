import { useEffect } from "react";
import { Routes, Route, Navigate } from "react-router";
import { useAuthStore } from "@/store/auth";
import { useSetupStatus } from "@/api/hooks";
import { AppLayout } from "@/components/layout/AppLayout";
import { ProtectedRoute } from "@/components/layout/ProtectedRoute";
import { Spinner } from "@/components/common";
import Login from "@/pages/Login";
import Home from "@/pages/Home";
import Movies from "@/pages/Movies";
import Series from "@/pages/Series";
import ItemDetail from "@/pages/ItemDetail";
import Search from "@/pages/Search";
import LiveTV from "@/pages/LiveTV";
import Settings from "@/pages/Settings";
import NotFound from "@/pages/NotFound";
import SetupWizard from "@/pages/setup/SetupWizard";
import AdminLayout from "@/pages/admin/AdminLayout";
import LibrariesAdmin from "@/pages/admin/LibrariesAdmin";
import UsersAdmin from "@/pages/admin/UsersAdmin";
import SystemAdmin from "@/pages/admin/SystemAdmin";
import ProvidersAdmin from "@/pages/admin/ProvidersAdmin";

export function App() {
  const loadFromStorage = useAuthStore((s) => s.loadFromStorage);
  const { data: setupStatus, isLoading } = useSetupStatus();

  useEffect(() => {
    loadFromStorage();
  }, [loadFromStorage]);

  if (isLoading) {
    return (
      <div className="flex h-dvh items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const needsSetup = setupStatus?.needs_setup ?? false;

  return (
    <Routes>
      {/* Setup wizard — only accessible when setup is needed */}
      <Route
        path="/setup/*"
        element={
          needsSetup ? <SetupWizard initialStep={setupStatus?.current_step} /> : <Navigate to="/login" replace />
        }
      />

      {/* Login */}
      <Route
        path="/login"
        element={needsSetup ? <Navigate to="/setup" replace /> : <Login />}
      />

      {/* Protected app routes */}
      <Route
        element={
          needsSetup ? (
            <Navigate to="/setup" replace />
          ) : (
            <ProtectedRoute />
          )
        }
      >
        <Route element={<AppLayout />}>
          <Route index element={<Home />} />
          <Route path="movies" element={<Movies />} />
          <Route path="series" element={<Series />} />
          <Route path="movies/:id" element={<ItemDetail />} />
          <Route path="series/:id" element={<ItemDetail />} />
          <Route path="items/:id" element={<ItemDetail />} />
          <Route path="search" element={<Search />} />
          <Route path="live-tv" element={<LiveTV />} />
          <Route path="settings" element={<Settings />} />

          {/* Admin routes */}
          <Route
            path="admin"
            element={<ProtectedRoute adminOnly />}
          >
            <Route element={<AdminLayout />}>
              <Route index element={<Navigate to="libraries" replace />} />
              <Route path="libraries" element={<LibrariesAdmin />} />
              <Route path="providers" element={<ProvidersAdmin />} />
              <Route path="users" element={<UsersAdmin />} />
              <Route path="system" element={<SystemAdmin />} />
            </Route>
          </Route>
        </Route>
      </Route>

      {/* 404 */}
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}
