import { Navigate, Outlet } from 'react-router';
import { useAuthStore } from '@/store/auth';

// ─── Props ──────────────────────────────────────────────────────────────────

interface ProtectedRouteProps {
  adminOnly?: boolean;
}

// ─── ProtectedRoute ─────────────────────────────────────────────────────────

export function ProtectedRoute({ adminOnly = false }: ProtectedRouteProps) {
  const { user, isAuthenticated } = useAuthStore();

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  if (adminOnly && user?.role !== 'admin') {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
