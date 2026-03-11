import { useState } from "react";
import { useAuthActions, useAuthState } from "../contexts/AuthContext";

export function Login() {
  const { error, loading } = useAuthState();
  const { login, clearError } = useAuthActions();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    clearError();
    setSubmitError(null);
    if (!email.trim() || !password) {
      setSubmitError("Email and password are required.");
      return;
    }
    setSubmitting(true);
    try {
      await login(email.trim(), password);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Login failed.");
    } finally {
      setSubmitting(false);
    }
  };

  const err = submitError ?? error;

  if (loading) {
    return (
      <div className="auth-screen">
        <div className="auth-card">
          <p className="auth-muted">Loading…</p>
        </div>
      </div>
    );
  }

  return (
    <div className="auth-screen">
      <div className="auth-card">
        <h1 className="auth-title">Sign in</h1>
        <p className="auth-sub">You’re already set up. Sign in to continue.</p>
        <form onSubmit={handleSubmit} className="auth-form">
          <label className="auth-label">
            Email
            <input
              type="email"
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="auth-input"
            />
          </label>
          <label className="auth-label">
            Password
            <input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="auth-input"
            />
          </label>
          {err && <p className="auth-error">{err}</p>}
          <button type="submit" className="auth-submit" disabled={submitting}>
            {submitting ? "Signing in…" : "Sign in"}
          </button>
        </form>
      </div>
    </div>
  );
}
