import { useEffect, lazy, Suspense } from "react";
import { Routes, Route, Navigate } from "react-router";
import { useAuthStore } from "@/store/auth";
import { useSetupStatus } from "@/api/hooks";
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
const LibrariesAdmin = lazy(() => import("@/pages/admin/LibrariesAdmin"));
const UsersAdmin = lazy(() => import("@/pages/admin/UsersAdmin"));
const SystemAdmin = lazy(() => import("@/pages/admin/SystemAdmin"));
const ProvidersAdmin = lazy(() => import("@/pages/admin/ProvidersAdmin"));

function LazyFallback() {
  return (
    <div className="flex h-32 items-center justify-center">
      <Spinner size="md" />
    </div>
  );
}

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
    </Suspense>
    </ErrorBoundary>
  );
}
