import { useEffect, useMemo, useRef, useState } from 'react'
import type { MediaItem } from '../api'
import { BASE_URL } from '../api'
import { mediaStreamUrl } from '@plum/shared'

interface Props {
  selected?: MediaItem
  wsConnected: boolean
  lastEvent?: string
}

export function PlayerPanel({ selected, wsConnected, lastEvent }: Props) {
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const [selectedTrackKey, setSelectedTrackKey] = useState<string>('off')

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

  const getBackdropURL = (path: string | undefined) => {
    if (!path) return ''
    return `https://image.tmdb.org/t/p/w500${path}`
  }

  return (
    <section className="player-panel">
      <header className="player-header">
        <div className="status-dot" data-connected={wsConnected} />
        <span className="status-label">
          WebSocket {wsConnected ? 'connected' : 'disconnected'}
        </span>
      </header>
      {selected ? (
        <div className="player-body">
          {selected.backdrop_path && (
            <div className="player-backdrop">
              <img src={getBackdropURL(selected.backdrop_path)} alt="backdrop" />
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

