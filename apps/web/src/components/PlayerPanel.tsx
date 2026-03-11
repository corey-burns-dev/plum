import { useEffect, useMemo, useRef, useState } from 'react'
import type { MediaItem } from '../api'
import { BASE_URL } from '../api'
import { mediaStreamUrl, tmdbBackdropUrl } from '@plum/shared'
import { usePlayer } from '../contexts/PlayerContext'

function TheatreIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <rect x="2" y="4" width="20" height="14" rx="2" />
      <path d="M2 8h20" />
    </svg>
  )
}

function FullscreenIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M8 3H5a2 2 0 0 0-2 2v3M21 8V5a2 2 0 0 0-2-2h-3M3 16v3a2 2 0 0 0 2 2h3M16 21h3a2 2 0 0 0 2-2v-3" />
    </svg>
  )
}

function FullscreenExitIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M8 3v3a2 2 0 0 1-2 2H3M21 8h-3a2 2 0 0 1-2-2V3M3 16h3a2 2 0 0 1 2 2v3M16 21v-3a2 2 0 0 1 2-2h3" />
    </svg>
  )
}

interface Props {
  selected?: MediaItem
  wsConnected: boolean
  lastEvent?: string
}

export function PlayerPanel({ selected, wsConnected, lastEvent }: Props) {
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const containerRef = useRef<HTMLElement | null>(null)
  const { viewMode, setViewMode } = usePlayer()
  const [selectedTrackKey, setSelectedTrackKey] = useState<string>('off')

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const onFullscreenChange = () => {
      if (!document.fullscreenElement) {
        setViewMode((m) => (m === 'fullscreen' ? 'theatre' : m))
      }
    }
    document.addEventListener('fullscreenchange', onFullscreenChange)
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange)
  }, [setViewMode])

  const enterFullscreen = () => {
    const el = containerRef.current
    if (el?.requestFullscreen) {
      setViewMode('fullscreen')
      el.requestFullscreen().catch(() => setViewMode('theatre'))
    }
  }

  const allTracks = useMemo(() => {
    if (!selected) return []
    const external =
      selected.subtitles?.map((sub) => ({
        key: `ext-${sub.id}`,
        label: sub.title || sub.language,
        src: `${BASE_URL || ''}/api/subtitles/${sub.id}`,
      })) ?? []
    const embedded =
      selected.embeddedSubtitles?.map((sub) => ({
        key: `emb-${sub.streamIndex}`,
        label: sub.title || sub.language,
        src: `${BASE_URL || ''}/api/media/${selected.id}/subtitles/embedded/${sub.streamIndex}`,
      })) ?? []
    return [...external, ...embedded]
  }, [selected])

  useEffect(() => {
    // Reset selection when media item changes.
    setSelectedTrackKey('off')
    const video = videoRef.current
    if (!video) return
    for (let i = 0; i < video.textTracks.length; i += 1) {
      video.textTracks[i].mode = 'disabled'
    }
  }, [selected])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    if (selectedTrackKey === 'off') {
      for (let i = 0; i < video.textTracks.length; i += 1) {
        video.textTracks[i].mode = 'disabled'
      }
      return
    }
    // Enable the matching track by label, disable others.
    for (let i = 0; i < video.textTracks.length; i += 1) {
      const track = video.textTracks[i]
      const isMatch = track.label === selectedTrackKey
      track.mode = isMatch ? 'showing' : 'disabled'
    }
  }, [selectedTrackKey])

  return (
    <section
      ref={containerRef}
      className={`player-panel player-panel--${viewMode}`}
      data-view-mode={viewMode}
    >
      <header className="player-header">
        <div className="status-dot" data-connected={wsConnected} />
        <span className="status-label">
          WebSocket {wsConnected ? 'connected' : 'disconnected'}
        </span>
        {selected && (
          <div className="player-view-mode-buttons">
            <button
              type="button"
              className="player-view-mode-btn"
              onClick={() => setViewMode(viewMode === 'theatre' ? 'inline' : 'theatre')}
              title={viewMode === 'theatre' ? 'Exit theatre mode' : 'Theatre mode'}
              aria-label={viewMode === 'theatre' ? 'Exit theatre mode' : 'Theatre mode'}
            >
              <TheatreIcon />
            </button>
            <button
              type="button"
              className="player-view-mode-btn"
              onClick={viewMode === 'fullscreen' ? () => document.exitFullscreen() : enterFullscreen}
              title={viewMode === 'fullscreen' ? 'Exit fullscreen' : 'Fullscreen'}
              aria-label={viewMode === 'fullscreen' ? 'Exit fullscreen' : 'Fullscreen'}
            >
              {viewMode === 'fullscreen' ? <FullscreenExitIcon /> : <FullscreenIcon />}
            </button>
          </div>
        )}
      </header>
      {selected ? (
        <div className="player-body">
          {selected.backdrop_path && (
            <div className="player-backdrop">
              <img src={tmdbBackdropUrl(selected.backdrop_path)} alt="backdrop" />
            </div>
          )}
          <div className="player-title">{selected.title}</div>
          <div className="player-sub">
            {selected.type} · {selected.release_date}
          </div>
          {selected.overview && (
            <div className="player-overview">{selected.overview}</div>
          )}
          {allTracks.length > 0 && (
            <div className="player-sub subtitle-selector">
              <label>
                Subtitles:{' '}
                <select
                  value={selectedTrackKey}
                  onChange={(e) => setSelectedTrackKey(e.target.value)}
                >
                  <option value="off">Off</option>
                  {allTracks.map((track) => (
                    <option key={track.key} value={track.label}>
                      {track.label}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          )}
          <video
            className="player-video"
            controls
            ref={videoRef}
            src={mediaStreamUrl(BASE_URL, selected.id)}
          >
            {allTracks.map((track) => (
              <track
                key={track.key}
                kind="subtitles"
                src={track.src}
                label={track.label}
              />
            ))}
          </video>
          <div className="player-event">
            {lastEvent || 'Idle · trigger a transcode to see updates.'}
          </div>
        </div>
      ) : (
        <div className="player-empty">
          Select a media item on the left to inspect and transcode.
        </div>
      )}
    </section>
  )
}

