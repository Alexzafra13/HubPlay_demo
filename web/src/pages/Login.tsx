import { useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router";
import { useLogin } from "@/api/hooks";
import { useAuthStore } from "@/store/auth";
import { Button } from "@/components/common";
import { Input } from "@/components/common";

export default function Login() {
  const navigate = useNavigate();
  const login = useLogin();
  const setAuth = useAuthStore((s) => s.setAuth);

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    login.mutate(
      { username, password },
      {
        onSuccess(data) {
          setAuth(data.user, data.access_token, data.refresh_token);
          navigate("/");
        },
        onError(err) {
          setError(err.message || "Login failed. Check your credentials.");
        },
      },
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg-base px-4">
      <div className="w-full max-w-sm rounded-[--radius-lg] border border-border bg-bg-card p-8 shadow-lg">
        {/* Logo */}
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-accent/10">
            <svg
              className="h-7 w-7 text-accent"
              viewBox="0 0 24 24"
              fill="currentColor"
            >
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
          <h1 className="text-2xl font-bold text-text-primary">HubPlay</h1>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Input
            label="Username"
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Enter your username"
            autoComplete="username"
            required
          />

          <Input
            label="Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Enter your password"
            autoComplete="current-password"
            required
          />

          {error && (
            <p className="rounded-[--radius-sm] bg-error/10 px-3 py-2 text-sm text-error">
              {error}
            </p>
          )}

          <Button
            type="submit"
            fullWidth
            size="lg"
            isLoading={login.isPending}
            className="mt-2"
          >
            Sign In
          </Button>
        </form>
      </div>
    </div>
  );
}
