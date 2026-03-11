import { useRef } from 'react'
import { mediaStreamUrl } from '@plum/shared'
import { Repeat, Shuffle, SkipBack, SkipForward } from 'lucide-react'
import { BASE_URL } from '../api'
import { usePlayer } from '../contexts/PlayerContext'

export function MusicPlayerBar() {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const {
    selectedMedia,
    musicQueue,
    musicQueueIndex,
    musicShuffle,
    musicRepeatMode,
    playNextTrack,
    playPreviousTrack,
    toggleMusicShuffle,
    cycleMusicRepeatMode,
  } = usePlayer()

  if (!selectedMedia || selectedMedia.type !== 'music') {
    return null
  }

  const queueCount = musicQueue.length
  const repeatLabel =
    musicRepeatMode === 'one'
      ? 'Repeat track'
      : musicRepeatMode === 'all'
        ? 'Repeat queue'
        : 'Repeat off'

  return (
    <section className="music-player-bar" aria-label="Music player">
      <div className="music-player-meta">
        <div className="music-player-title">{selectedMedia.title}</div>
        <div className="music-player-subtitle">
          {selectedMedia.artist || 'Unknown Artist'}
          {selectedMedia.album ? ` • ${selectedMedia.album}` : ''}
          {queueCount > 0 ? ` • ${musicQueueIndex + 1}/${queueCount}` : ''}
        </div>
      </div>
      <div className="music-player-controls">
        <button
          type="button"
          className={`music-player-button${musicShuffle ? ' is-active' : ''}`}
          onClick={toggleMusicShuffle}
          aria-label={musicShuffle ? 'Disable shuffle' : 'Enable shuffle'}
        >
          <Shuffle className="size-4" />
        </button>
        <button
          type="button"
          className="music-player-button"
          onClick={playPreviousTrack}
          aria-label="Previous track"
        >
          <SkipBack className="size-4" />
        </button>
        <audio
          key={selectedMedia.id}
          ref={audioRef}
          className="music-player-audio"
          controls
          autoPlay
          onEnded={() => {
            if (musicRepeatMode === 'one' && audioRef.current) {
              audioRef.current.currentTime = 0
              void audioRef.current.play().catch(() => {})
              return
            }
            playNextTrack()
          }}
        >
          <source src={mediaStreamUrl(BASE_URL, selectedMedia.id)} />
        </audio>
        <button
          type="button"
          className="music-player-button"
          onClick={playNextTrack}
          aria-label="Next track"
        >
          <SkipForward className="size-4" />
        </button>
        <button
          type="button"
          className={`music-player-button${musicRepeatMode !== 'off' ? ' is-active' : ''}`}
          onClick={cycleMusicRepeatMode}
          aria-label={repeatLabel}
          title={repeatLabel}
        >
          <Repeat className="size-4" />
          <span className="music-player-repeat-copy">
            {musicRepeatMode === 'one' ? '1' : musicRepeatMode === 'all' ? 'all' : 'off'}
          </span>
        </button>
      </div>
    </section>
  )
}
