import { Navigate, Outlet } from 'react-router';
import { useSetupStatus } from '@/api/hooks';

// ─── SetupRoute ─────────────────────────────────────────────────────────────

export function SetupRoute() {
  const { data, isLoading } = useSetupStatus();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-bg-base">
        <svg
          className="animate-spin h-8 w-8 text-accent"
          viewBox="0 0 24 24"
          fill="none"
        >
          <circle
            className="opacity-25"
            cx="12"
            cy="12"
            r="10"
            stroke="currentColor"
            strokeWidth="4"
          />
          <path
            className="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
          />
        </svg>
      </div>
    );
  }

  // Setup already completed — redirect to login
  if (data && !data.needs_setup) {
    return <Navigate to="/login" replace />;
  }

  return <Outlet />;
}
