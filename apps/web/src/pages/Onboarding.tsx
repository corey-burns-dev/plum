import { useEffect, useState } from "react";
import type { LibraryType } from "../api";
import { createAdmin, createLibrary, getLibraryScanStatus, startLibraryScan } from "../api";
import { useAuthActions } from "../contexts/AuthContext";
import { getLibraryActivity, getLibraryActivityLabel } from "../lib/libraryActivity";

type Step = "admin" | "library";

type AddedLibrary = {
  id: number;
  name: string;
  type: LibraryType;
  path: string;
  phase: "queued" | "scanning" | "completed" | "failed";
  enriching: boolean;
  addedCount: number;
  updatedCount: number;
  removedCount: number;
  unmatchedCount: number;
  skippedCount: number;
  error?: string;
};

type OnboardingProps = {
  onGoToHome: () => void;
};

const LIBRARY_TYPE_OPTIONS: { value: LibraryType; label: string }[] = [
  { value: "tv", label: "TV shows" },
  { value: "movie", label: "Movies" },
  { value: "anime", label: "Anime" },
  { value: "music", label: "Music" },
];

const DEFAULT_LIBRARIES: { name: string; type: LibraryType; path: string }[] = [
  { name: "TV", type: "tv", path: "/tv" },
  { name: "Movies", type: "movie", path: "/movies" },
  { name: "Anime", type: "anime", path: "/anime" },
  { name: "Music", type: "music", path: "/music" },
];

const SCAN_STATUS_POLL_INTERVAL_MS = 2_000;

function mergeLibraryScanStatus(
  library: AddedLibrary,
  status: Awaited<ReturnType<typeof getLibraryScanStatus>>,
): AddedLibrary {
  return {
    ...library,
    phase: status.phase === "idle" ? "queued" : status.phase,
    enriching: status.enriching,
    addedCount: status.added,
    updatedCount: status.updated,
    removedCount: status.removed,
    unmatchedCount: status.unmatched,
    skippedCount: status.skipped,
    error: status.error,
  };
}

export function Onboarding({ onGoToHome }: OnboardingProps) {
  const { refreshMe } = useAuthActions();
  const [step, setStep] = useState<Step>("admin");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [libraryType, setLibraryType] = useState<LibraryType>("tv");
  const [libraryName, setLibraryName] = useState("");
  const [libraryPath, setLibraryPath] = useState("");
  const [addedLibraries, setAddedLibraries] = useState<AddedLibrary[]>([]);
  const [loading, setLoading] = useState(false);
  const [addingDefaults, setAddingDefaults] = useState(false);
  const [showManualLibraryForm, setShowManualLibraryForm] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const pendingLibraries = addedLibraries.filter(
      (library) => library.phase === "queued" || library.phase === "scanning" || library.enriching,
    );
    if (pendingLibraries.length === 0) return;

    let cancelled = false;
    const refreshStatuses = async () => {
      const settled = await Promise.allSettled(
        pendingLibraries.map(async (library) => [library.id, await getLibraryScanStatus(library.id)] as const),
      );
      if (cancelled) return;

      const statuses = new Map<number, Awaited<ReturnType<typeof getLibraryScanStatus>>>();
      for (const entry of settled) {
        if (entry.status === "fulfilled") {
          statuses.set(entry.value[0], entry.value[1]);
        }
      }

      setAddedLibraries((current) =>
        current.map((library) => {
          const status = statuses.get(library.id);
          return status ? mergeLibraryScanStatus(library, status) : library;
        }),
      );
    };

    void refreshStatuses();
    const intervalId = window.setInterval(() => {
      void refreshStatuses();
    }, SCAN_STATUS_POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      window.clearInterval(intervalId);
    };
  }, [addedLibraries]);

  const handleAdminSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < 10) {
      setError("Password must be at least 10 characters.");
      return;
    }
    if (password !== confirmPassword) {
      setError("Passwords do not match.");
      return;
    }
    setLoading(true);
    try {
      await createAdmin({ email: email.trim(), password });
      setStep("library");
      refreshMe().catch(() => {}); // update user/session in background; don't block transition
    } catch (err) {
      setError(err instanceof Error ? err.message : "Setup failed.");
    } finally {
      setLoading(false);
    }
  };

  const handleQuickStartAdmin = async () => {
    if (!import.meta.env.DEV) return;
    if (loading) return;

    setError(null);
    setLoading(true);
    try {
      const quickEmail = "admin@example.com";
      const quickPassword = "passwordpassword";
      await createAdmin({ email: quickEmail, password: quickPassword });
      setStep("library");
      refreshMe().catch(() => {}); // update user/session in background; don't block transition
    } catch (err) {
      setError(err instanceof Error ? err.message : "Quick start failed.");
    } finally {
      setLoading(false);
    }
  };

  const handleAddLibrary = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!libraryName.trim() || !libraryPath.trim()) {
      setError("Library name and path are required.");
      return;
    }
    setLoading(true);
    try {
      const lib = await createLibrary({
        name: libraryName.trim(),
        type: libraryType,
        path: libraryPath.trim(),
      });
      const status = await startLibraryScan(lib.id, { identify: false });
      setAddedLibraries((prev) => [
        ...prev,
        mergeLibraryScanStatus(
          {
            id: lib.id,
            name: lib.name,
            type: lib.type,
            path: lib.path,
            phase: "queued",
            enriching: false,
            addedCount: 0,
            updatedCount: 0,
            removedCount: 0,
            unmatchedCount: 0,
            skippedCount: 0,
          },
          status,
        ),
      ]);
      setLibraryName("");
      setLibraryPath("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Library or scan failed.");
    } finally {
      setLoading(false);
    }
  };

  const handleAddDefaultLibraries = async () => {
    setError(null);
    setAddingDefaults(true);
    try {
      const existingPaths = new Set(addedLibraries.map((l) => l.path));
      const createdLibraries: AddedLibrary[] = [];
      for (const def of DEFAULT_LIBRARIES) {
        if (existingPaths.has(def.path)) continue;
        const lib = await createLibrary({ name: def.name, type: def.type, path: def.path });
        const nextLibrary: AddedLibrary = {
          id: lib.id,
          name: lib.name,
          type: lib.type,
          path: lib.path,
          phase: "queued",
          enriching: false,
          addedCount: 0,
          updatedCount: 0,
          removedCount: 0,
          unmatchedCount: 0,
          skippedCount: 0,
          error: undefined,
        };
        createdLibraries.push(nextLibrary);
        existingPaths.add(def.path);
      }
      const scannedLibraries = await Promise.all(
        createdLibraries.map(async (library) => {
          try {
            const status = await startLibraryScan(library.id, { identify: false });
            return mergeLibraryScanStatus(library, status);
          } catch {
            // Path may not exist (e.g. /anime not mounted); library is still created.
            return library;
          }
        }),
      );
      if (createdLibraries.length > 0) {
        setAddedLibraries((prev) => [...prev, ...scannedLibraries]);
      }
      if (existingPaths.size > 0) {
        onGoToHome();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add default libraries.");
    } finally {
      setAddingDefaults(false);
    }
  };

  const handleFinishSetup = () => {
    onGoToHome();
  };

  return (
    <div className="auth-screen">
      <div className="onboarding-wizard">
        <div className="wizard-progress">
          <span className={step === "admin" ? "active" : "done"}>1. Admin</span>
          <span className={step === "library" ? "active" : ""}>2. Library</span>
        </div>

        {step === "admin" && (
          <div className="auth-card">
            <h1 className="auth-title">Create admin account</h1>
            <p className="auth-sub">Set up the first user for your Plum server.</p>
            <form onSubmit={handleAdminSubmit} className="auth-form">
              <label className="auth-label">
                Email
                <input
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="auth-input"
                  required
                />
              </label>
              <label className="auth-label">
                Password (min 10 characters)
                <input
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="auth-input"
                  minLength={10}
                  required
                />
              </label>
              <label className="auth-label">
                Confirm password
                <input
                  type="password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  className="auth-input"
                  required
                />
              </label>
              {error && <p className="auth-error">{error}</p>}
              <button type="submit" className="auth-submit" disabled={loading}>
                {loading ? "Creating…" : "Create admin"}
              </button>
              {import.meta.env.DEV && (
                <button
                  type="button"
                  className="auth-submit secondary"
                  disabled={loading}
                  onClick={handleQuickStartAdmin}
                >
                  Quick start with default admin
                </button>
              )}
            </form>
          </div>
        )}

        {step === "library" && (
          <div className="auth-card">
            <h1 className="auth-title">Add libraries</h1>
            <p className="auth-sub">
              Start with the full default library set so Plum can import TV, movies, anime, and
              music right away. You can still add libraries manually if you want a custom setup.
            </p>
            <div className="onboarding-library-actions" style={{ marginBottom: "1rem" }}>
              <button
                type="button"
                className="auth-submit"
                disabled={loading || addingDefaults}
                onClick={handleAddDefaultLibraries}
              >
                {addingDefaults ? "Adding…" : "Add default libraries and continue"}
              </button>
              <button
                type="button"
                className="auth-submit secondary"
                disabled={loading || addingDefaults}
                onClick={() => setShowManualLibraryForm((current) => !current)}
              >
                {showManualLibraryForm ? "Hide manual setup" : "Add libraries manually instead"}
              </button>
            </div>
            {showManualLibraryForm && (
              <form onSubmit={handleAddLibrary} className="auth-form">
                <label className="auth-label">
                  Library type
                  <select
                    value={libraryType}
                    onChange={(e) => setLibraryType(e.target.value as LibraryType)}
                    className="auth-input"
                  >
                    {LIBRARY_TYPE_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="auth-label">
                  Library name
                  <input
                    type="text"
                    value={libraryName}
                    onChange={(e) => setLibraryName(e.target.value)}
                    className="auth-input"
                    placeholder="e.g. Shows (TV), Movies, Shows (anime)"
                    required
                  />
                </label>
                <label className="auth-label">
                  Folder path (on the server)
                  <input
                    type="text"
                    value={libraryPath}
                    onChange={(e) => setLibraryPath(e.target.value)}
                    className="auth-input"
                    placeholder="/path/to/folder"
                    required
                  />
                </label>
                {error && <p className="auth-error">{error}</p>}
                <div className="onboarding-library-actions">
                  <button type="submit" className="auth-submit" disabled={loading || addingDefaults}>
                    {loading ? "Adding…" : "Add library"}
                  </button>
                  <button
                    type="button"
                    className="auth-submit secondary"
                    disabled={addedLibraries.length === 0 || loading || addingDefaults}
                    onClick={handleFinishSetup}
                  >
                    Finish setup
                  </button>
                </div>
              </form>
            )}
            {!showManualLibraryForm && error && <p className="auth-error">{error}</p>}
            {addedLibraries.length > 0 && (
              <div className="onboarding-libraries-summary">
                <p className="auth-sub">Added libraries:</p>
                <ul className="onboarding-libraries-list">
                  {addedLibraries.map((lib) => (
                    <li key={lib.id}>
                      <strong>{lib.name}</strong> ({lib.type}) —{" "}
                      {(
                        getLibraryActivityLabel(
                          getLibraryActivity({
                            scanPhase: lib.phase,
                            enriching: lib.enriching,
                          }),
                        ) ?? lib.phase
                      ).toLowerCase()}
                      {lib.addedCount > 0 ? `, added ${lib.addedCount}` : ""}
                      {lib.updatedCount > 0 ? `, updated ${lib.updatedCount}` : ""}
                      {lib.unmatchedCount > 0 ? `, unmatched ${lib.unmatchedCount}` : ""}
                      {lib.skippedCount > 0 ? `, skipped ${lib.skippedCount}` : ""}
                      {lib.removedCount > 0 ? `, removed ${lib.removedCount}` : ""}
                      {lib.error ? ` — ${lib.error}` : ""}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
