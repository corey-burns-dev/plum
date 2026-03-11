import { Link, useParams } from 'react-router-dom'
import { useLibraries } from '@/queries'
import { getLibraryTabLabel } from '@/lib/showGrouping'
import { cn } from '@/lib/utils'
import { Film, Music, Tv } from 'lucide-react'
import type { Library } from '@/api'

function LibraryIcon({ lib }: { lib: Library }) {
  if (lib.type === 'music') return <Music className="size-4 shrink-0 opacity-70" />
  if (lib.type === 'movie') return <Film className="size-4 shrink-0 opacity-70" />
  return <Tv className="size-4 shrink-0 opacity-70" />
}

export function Sidebar() {
  const { libraryId } = useParams()
  const { data: libraries = [], isLoading } = useLibraries()
  const activeId = libraryId ? parseInt(libraryId, 10) : null

  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-[var(--plum-border)] bg-[var(--plum-panel)]/60">
      <nav className="flex flex-col gap-0.5 p-3" aria-label="Libraries">
        <div className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-[var(--plum-muted)]">
          Libraries
        </div>
        {isLoading ? (
          <div className="px-2 py-2 text-sm text-[var(--plum-muted)]">Loading…</div>
        ) : (
          libraries.map((lib) => {
            const isActive = activeId === lib.id
            return (
              <Link
                key={lib.id}
                to={`/library/${lib.id}`}
                className={cn(
                  'flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-[var(--plum-accent-soft)] text-[var(--plum-accent)]'
                    : 'text-[var(--plum-text)] hover:bg-[var(--plum-panel-alt)] hover:text-[var(--plum-text)]',
                )}
              >
                <LibraryIcon lib={lib} />
                <span className="truncate">{getLibraryTabLabel(lib)}</span>
              </Link>
            )
          })
        )}
      </nav>
    </aside>
  )
}
