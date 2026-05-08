import { Navigate, Outlet, useLocation } from 'react-router';
import { useAuthStore } from '@/store/auth';

// ─── Props ──────────────────────────────────────────────────────────────────

interface ProtectedRouteProps {
  adminOnly?: boolean;
}

// ─── ProtectedRoute ─────────────────────────────────────────────────────────

export function ProtectedRoute({ adminOnly = false }: ProtectedRouteProps) {
  const { user, isAuthenticated } = useAuthStore();
  const location = useLocation();

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  // Forced password rotation: when the admin created the account
  // with an auto-generated password (or just reset it), the user
  // lands on /change-password and can't escape until they rotate.
  // The check has to live here (not inside AppLayout) because the
  // shell renders rails / API calls that would 401 on a stolen
  // temp-password JWT — we want zero useful surface before rotation.
  if (
    user?.password_change_required &&
    location.pathname !== '/change-password'
  ) {
    return <Navigate to="/change-password" replace />;
  }

  if (adminOnly && user?.role !== 'admin') {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
