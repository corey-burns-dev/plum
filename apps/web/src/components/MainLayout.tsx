import { Outlet } from 'react-router-dom'
import { usePlayer } from '@/contexts/PlayerContext'
import { MusicPlayerBar } from './MusicPlayerBar'
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'
import { PlayerPanel } from './PlayerPanel'

export function MainLayout() {
  const { selectedMedia, wsConnected, lastEvent, viewMode } = usePlayer()
  const theatre = viewMode === 'theatre' || viewMode === 'fullscreen'
  const isMusicSelected = selectedMedia?.type === 'music'

  return (
    <div className="flex min-h-screen flex-col">
      <TopBar />
      <div className="flex flex-1 min-h-0">
        <Sidebar />
        <main
          className={`flex flex-1 flex-col min-w-0 ${theatre ? 'main--player-theatre' : ''}`}
        >
          <section className="main-content flex-1 overflow-auto p-4 md:p-6">
            <Outlet />
          </section>
          {isMusicSelected ? (
            <MusicPlayerBar />
          ) : (
            <PlayerPanel
              selected={selectedMedia ?? undefined}
              wsConnected={wsConnected}
              lastEvent={lastEvent}
            />
          )}
        </main>
      </div>
    </div>
  )
}
