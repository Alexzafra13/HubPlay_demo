import { Link } from "react-router";
import { Button } from "@/components/common";

export default function NotFound() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 px-4 text-center">
      <h1 className="text-7xl font-bold text-text-muted">404</h1>
      <p className="text-xl text-text-secondary">Page not found</p>
      <p className="max-w-sm text-sm text-text-muted">
        The page you are looking for doesn't exist or has been moved.
      </p>
      <Link to="/" className="mt-4">
        <Button size="lg">Go Home</Button>
      </Link>
    </div>
  );
}
