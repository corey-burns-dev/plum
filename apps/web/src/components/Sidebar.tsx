import { Link, useLocation, useParams } from "react-router-dom";
import { useLibraries } from "@/queries";
import { getLibraryTabLabel } from "@/lib/showGrouping";
import { cn } from "@/lib/utils";
import { Compass, Film, Home, Music, Tv } from "lucide-react";
import type { Library } from "@/api";
import { useIdentifyQueue } from "@/contexts/IdentifyQueueContext";
import { useScanQueue } from "@/contexts/ScanQueueContext";

function LibraryIcon({ lib }: { lib: Library }) {
  if (lib.type === "music") return <Music className="size-[18px] shrink-0 opacity-70" />;
  if (lib.type === "movie") return <Film className="size-[18px] shrink-0 opacity-70" />;
  return <Tv className="size-[18px] shrink-0 opacity-70" />;
}

export function Sidebar() {
  const { libraryId } = useParams();
  const { data: libraries = [], isLoading } = useLibraries();
  const { getLibraryPhase } = useIdentifyQueue();
  const { getLibraryScanStatus } = useScanQueue();
  const location = useLocation();
  const activeId = libraryId ? parseInt(libraryId, 10) : null;
  const isHomeRoute = location.pathname === "/";
  const isDiscoverRoute = location.pathname === "/discover" || location.pathname.startsWith("/discover/");

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-[var(--plum-border)] bg-[var(--plum-panel)]/60 overflow-y-auto">
      <nav className="flex flex-col gap-0.5 p-3" aria-label="Libraries">
        <Link
          to="/"
          className={cn(
            "mb-2 flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium transition-colors",
            isHomeRoute
              ? "bg-[var(--plum-accent-soft)] text-[var(--plum-accent)]"
              : "text-[var(--plum-text)] hover:bg-[var(--plum-panel-alt)] hover:text-[var(--plum-text)]",
          )}
        >
          <Home className="size-[18px] shrink-0 opacity-70" />
          <span className="truncate">Home</span>
        </Link>
        <Link
          to="/discover"
          className={cn(
            "mb-2 flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium transition-colors",
            isDiscoverRoute
              ? "bg-[var(--plum-accent-soft)] text-[var(--plum-accent)]"
              : "text-[var(--plum-text)] hover:bg-[var(--plum-panel-alt)] hover:text-[var(--plum-text)]",
          )}
        >
          <Compass className="size-[18px] shrink-0 opacity-70" />
          <span className="truncate">Discover</span>
        </Link>
        <div className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-[var(--plum-muted)]">
          Libraries
        </div>
        {isLoading ? (
          <div className="px-2 py-2 text-sm text-[var(--plum-muted)]">Loading…</div>
        ) : (
          libraries.map((lib) => {
            const isActive = activeId === lib.id;
            const identifyPhase = getLibraryPhase(lib.id);
            const scanStatus = getLibraryScanStatus(lib.id);
            const scanPhase = scanStatus?.phase;
            const isEnriching = scanStatus?.enriching === true;
            const isAutoIdentifying =
              scanStatus?.identifyPhase === "queued" || scanStatus?.identifyPhase === "identifying";
            const isIdentifying =
              identifyPhase === "identifying" || identifyPhase === "soft-reveal";
            const isScanning =
              scanPhase === "queued" ||
              scanPhase === "scanning" ||
              isEnriching ||
              isAutoIdentifying;
            return (
              <Link
                key={lib.id}
                to={`/library/${lib.id}`}
                className={cn(
                  "flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-[var(--plum-accent-soft)] text-[var(--plum-accent)]"
                    : "text-[var(--plum-text)] hover:bg-[var(--plum-panel-alt)] hover:text-[var(--plum-text)]",
                  (isIdentifying || isScanning) &&
                    "shadow-[inset_0_0_0_1px_rgba(244,90,160,0.2),0_0_18px_rgba(244,90,160,0.14)]",
                )}
              >
                <LibraryIcon lib={lib} />
                <span className="truncate">{getLibraryTabLabel(lib)}</span>
                {(isIdentifying || isScanning) && (
                  <span
                    className="relative ml-auto flex size-2.5 shrink-0 items-center justify-center"
                    data-testid={`library-identifying-${lib.id}`}
                    aria-hidden="true"
                  >
                    <span className="absolute inline-flex size-full animate-ping rounded-full bg-[var(--plum-accent)] opacity-45" />
                    <span className="relative size-2 rounded-full bg-[var(--plum-accent)] shadow-[0_0_10px_var(--plum-accent)]" />
                  </span>
                )}
              </Link>
            );
          })
        )}
      </nav>
    </aside>
  );
}
