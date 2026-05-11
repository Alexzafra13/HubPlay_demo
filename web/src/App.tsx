import { useEffect, Suspense } from "react";
import { Routes, Route, Navigate, useNavigate } from "react-router";
import { useAuthStore } from "@/store/auth";
import { useSetupStatus } from "@/api/hooks";
import { api } from "@/api/client";
import { AppLayout } from "@/components/layout/AppLayout";
import { ProtectedRoute } from "@/components/layout/ProtectedRoute";
import { Spinner, ErrorBoundary } from "@/components/common";
import { DebugOverlay } from "@/components/common/DebugOverlay";
import { lazyWithRetry } from "@/utils/lazyWithRetry";
import Login from "@/pages/Login";
const ChangePassword = lazyWithRetry(() => import("@/pages/ChangePassword"));
const WhoIsWatching = lazyWithRetry(() => import("@/pages/WhoIsWatching"));

// Lazy-loaded routes via lazyWithRetry: when a chunk 404s after a
// deploy (stale tab references the previous build's hashes), the
// helper triggers one hard reload so the new index.html resolves.
const Home = lazyWithRetry(() => import("@/pages/Home"));
const Movies = lazyWithRetry(() => import("@/pages/Movies"));
const Series = lazyWithRetry(() => import("@/pages/Series"));
const ItemDetail = lazyWithRetry(() => import("@/pages/ItemDetail"));
const PersonDetail = lazyWithRetry(() => import("@/pages/PersonDetail"));
const StudioDetail = lazyWithRetry(() => import("@/pages/StudioDetail"));
const CollectionDetail = lazyWithRetry(() => import("@/pages/CollectionDetail"));
const Collections = lazyWithRetry(() => import("@/pages/Collections"));
const Search = lazyWithRetry(() => import("@/pages/Search"));
const LiveTV = lazyWithRetry(() => import("@/pages/LiveTV"));
const Settings = lazyWithRetry(() => import("@/pages/Settings"));
const NotFound = lazyWithRetry(() => import("@/pages/NotFound"));
const SetupWizard = lazyWithRetry(() => import("@/pages/setup/SetupWizard"));
const AdminLayout = lazyWithRetry(() => import("@/pages/admin/AdminLayout"));
const DashboardAdmin = lazyWithRetry(() => import("@/pages/admin/DashboardAdmin"));
const LibrariesAdmin = lazyWithRetry(() => import("@/pages/admin/LibrariesAdmin"));
const LibraryNewPage = lazyWithRetry(() => import("@/pages/admin/librariesAdmin/LibraryNewPage"));
const LibraryDetailPage = lazyWithRetry(() => import("@/pages/admin/librariesAdmin/LibraryDetailPage"));
const UsersAdmin = lazyWithRetry(() => import("@/pages/admin/UsersAdmin"));
const ProvidersAdmin = lazyWithRetry(() => import("@/pages/admin/ProvidersAdmin"));
const FederationAdmin = lazyWithRetry(() => import("@/pages/admin/FederationAdmin"));
const PeersPage = lazyWithRetry(() => import("@/pages/PeersPage"));
const PeerLibrariesPage = lazyWithRetry(() => import("@/pages/PeerLibrariesPage"));
const PeerLibraryItemsPage = lazyWithRetry(() => import("@/pages/PeerLibraryItemsPage"));
const PeerItemDetail = lazyWithRetry(() => import("@/pages/PeerItemDetail"));
const LinkDevice = lazyWithRetry(() => import("@/pages/LinkDevice"));
const PairThisDevice = lazyWithRetry(() => import("@/pages/PairThisDevice"));
const SystemStatus = lazyWithRetry(() => import("@/pages/admin/system/SystemStatus"));

function LazyFallback() {
  return (
    <div className="flex h-32 items-center justify-center">
      <Spinner size="md" />
    </div>
  );
}

export function App() {
  const bootstrap = useAuthStore((s) => s.bootstrap);
  const bootstrapped = useAuthStore((s) => s.bootstrapped);
  const logout = useAuthStore((s) => s.logout);
  const navigate = useNavigate();
  const { data: setupStatus, isLoading } = useSetupStatus();

  // Bootstrap once: hydrate from localStorage and refresh the access
  // cookie BEFORE any protected query fires, so a returning user
  // never produces the "401 on every initial query → silent retry"
  // sequence that previously polluted the dev console.
  useEffect(() => {
    void bootstrap();
  }, [bootstrap]);

  // Wire ApiClient auth events to Zustand store + React Router
  useEffect(() => {
    api.setAuthListener({
      onAuthFailure: () => {
        logout();
        navigate("/login", { replace: true });
      },
    });
  }, [logout, navigate]);

  if (isLoading || !bootstrapped) {
    return (
      <div className="flex h-dvh items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  const needsSetup = setupStatus?.needs_setup ?? false;

  return (
    <ErrorBoundary>
    <DebugOverlay />
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

        {/* Public pairing UI — TVs / consoles opening HubPlay in
            their browser hit this to render a QR + user_code instead
            of typing a password on a remote. Unauthenticated; the
            poll-on-approval path sets cookies and routes home. */}
        <Route
          path="/pair"
          element={
            needsSetup ? <Navigate to="/setup" replace /> : <PairThisDevice />
          }
        />

        {/* Forced password change. Mounted outside the AppLayout
            tree so the page renders standalone (no TopBar / Sidebar
            chrome) and can't escape into the rest of the app while
            the user holds a temp-password JWT. ProtectedRoute redirects
            here when password_change_required is set. */}
        <Route
          path="/change-password"
          element={
            needsSetup ? <Navigate to="/setup" replace /> : <ChangePassword />
          }
        />

        {/* Profile picker. Mounted outside AppLayout for the same
            standalone-canvas reason ChangePassword has — clicking
            a profile commits a new JWT, then we navigate into the
            shell. */}
        <Route
          path="/select-profile"
          element={
            needsSetup ? <Navigate to="/setup" replace /> : <WhoIsWatching />
          }
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
            <Route path="people/:id" element={<PersonDetail />} />
            <Route path="studios/:slug" element={<StudioDetail />} />
            <Route path="collections" element={<Collections />} />
            <Route path="collections/:id" element={<CollectionDetail />} />
            <Route path="search" element={<Search />} />
            <Route path="live-tv" element={<LiveTV />} />
            <Route path="peers" element={<PeersPage />} />
            <Route path="peers/:peerId" element={<PeerLibrariesPage />} />
            <Route path="peers/:peerId/libraries/:libraryId" element={<PeerLibraryItemsPage />} />
            <Route path="peers/:peerId/libraries/:libraryId/items/:itemId" element={<PeerItemDetail />} />
            <Route path="link" element={<LinkDevice />} />
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
                <Route path="libraries/new" element={<LibraryNewPage />} />
                <Route path="libraries/:id" element={<LibraryDetailPage />} />
                {/* Providers used to be its own top-level tab. Folded
                    into the Library page as a section, but the URL
                    stays for bookmarks (and so deep-links from search
                    keep working) — it just renders the providers
                    surface without its own tab in the rail. */}
                <Route path="providers" element={<ProvidersAdmin />} />
                <Route path="users" element={<UsersAdmin />} />
                {/* Federation moved out of the top-level rail and
                    into the Users page. The dedicated route stays
                    so bookmarks survive; landing on it shows the
                    federation surface inline. */}
                <Route path="federation" element={<FederationAdmin />} />
                {/* System lost its three sub-tabs (status / activity /
                    advanced) — rendered as a single Settings-style
                    page now. Activity in particular was empty
                    placeholders that the audit log + dashboard cover
                    better. The legacy paths redirect so external
                    bookmarks survive. */}
                <Route path="system" element={<SystemStatus />} />
                <Route
                  path="system/status"
                  element={<Navigate to="/admin/system" replace />}
                />
                <Route
                  path="system/activity"
                  element={<Navigate to="/admin/dashboard" replace />}
                />
                <Route
                  path="system/advanced"
                  element={<Navigate to="/admin/system" replace />}
                />
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
