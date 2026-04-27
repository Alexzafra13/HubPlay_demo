import { useEffect, lazy, Suspense } from "react";
import { Routes, Route, Navigate, useNavigate } from "react-router";
import { useAuthStore } from "@/store/auth";
import { useSetupStatus } from "@/api/hooks";
import { api } from "@/api/client";
import { AppLayout } from "@/components/layout/AppLayout";
import { ProtectedRoute } from "@/components/layout/ProtectedRoute";
import { Spinner, ErrorBoundary } from "@/components/common";
import Login from "@/pages/Login";

// Lazy-loaded routes for code splitting
const Home = lazy(() => import("@/pages/Home"));
const Movies = lazy(() => import("@/pages/Movies"));
const Series = lazy(() => import("@/pages/Series"));
const ItemDetail = lazy(() => import("@/pages/ItemDetail"));
const Search = lazy(() => import("@/pages/Search"));
const LiveTV = lazy(() => import("@/pages/LiveTV"));
const Settings = lazy(() => import("@/pages/Settings"));
const NotFound = lazy(() => import("@/pages/NotFound"));
const SetupWizard = lazy(() => import("@/pages/setup/SetupWizard"));
const AdminLayout = lazy(() => import("@/pages/admin/AdminLayout"));
const DashboardAdmin = lazy(() => import("@/pages/admin/DashboardAdmin"));
const LibrariesAdmin = lazy(() => import("@/pages/admin/LibrariesAdmin"));
const UsersAdmin = lazy(() => import("@/pages/admin/UsersAdmin"));
const ProvidersAdmin = lazy(() => import("@/pages/admin/ProvidersAdmin"));
const SystemLayout = lazy(() => import("@/pages/admin/system/SystemLayout"));
const SystemStatus = lazy(() => import("@/pages/admin/system/SystemStatus"));
const SystemActivity = lazy(() => import("@/pages/admin/system/SystemActivity"));
const SystemAdvanced = lazy(() => import("@/pages/admin/system/SystemAdvanced"));

function LazyFallback() {
  return (
    <div className="flex h-32 items-center justify-center">
      <Spinner size="md" />
    </div>
  );
}

export function App() {
  const loadFromStorage = useAuthStore((s) => s.loadFromStorage);
  const logout = useAuthStore((s) => s.logout);
  const navigate = useNavigate();
  const { data: setupStatus, isLoading } = useSetupStatus();

  useEffect(() => {
    loadFromStorage();
  }, [loadFromStorage]);

  // Wire ApiClient auth events to Zustand store + React Router
  useEffect(() => {
    api.setAuthListener({
      onAuthFailure: () => {
        logout();
        navigate("/login", { replace: true });
      },
    });
  }, [logout, navigate]);

  if (isLoading) {
    return (
      <div className="flex h-dvh items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const needsSetup = setupStatus?.needs_setup ?? false;

  return (
    <ErrorBoundary>
    <Suspense fallback={<LazyFallback />}>
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

            {/* Admin routes.
                /admin              → Dashboard (landing).
                /admin/libraries    → existing per-domain pages.
                /admin/system/*     → nested sub-tabs (status / activity /
                                      advanced) under one outlet so the
                                      "System" tab can group server detail
                                      without overflowing into siblings. */}
            <Route
              path="admin"
              element={<ProtectedRoute adminOnly />}
            >
              <Route element={<AdminLayout />}>
                <Route index element={<Navigate to="dashboard" replace />} />
                <Route path="dashboard" element={<DashboardAdmin />} />
                <Route path="libraries" element={<LibrariesAdmin />} />
                <Route path="providers" element={<ProvidersAdmin />} />
                <Route path="users" element={<UsersAdmin />} />
                <Route path="system" element={<SystemLayout />}>
                  <Route index element={<Navigate to="status" replace />} />
                  <Route path="status" element={<SystemStatus />} />
                  <Route path="activity" element={<SystemActivity />} />
                  <Route path="advanced" element={<SystemAdvanced />} />
                </Route>
              </Route>
            </Route>
          </Route>
        </Route>

        {/* 404 */}
        <Route path="*" element={<NotFound />} />
      </Routes>
    </Suspense>
    </ErrorBoundary>
  );
}
