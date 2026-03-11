import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import {
  embeddedSubtitleUrl,
  externalSubtitleUrl,
  mediaStreamUrl,
  tmdbBackdropUrl,
  tmdbPosterUrl,
} from "@plum/shared";
import {
  Expand,
  Minimize2,
  Pause,
  Play,
  Repeat,
  Shuffle,
  SkipBack,
  SkipForward,
  Subtitles,
  Volume2,
  VolumeX,
  X,
} from "lucide-react";
import type { MediaItem } from "../api";
import { BASE_URL } from "../api";
import { usePlayer } from "../contexts/PlayerContext";

type PlaybackState = {
  currentTime: number;
  duration: number;
  isPlaying: boolean;
};

type SubtitleTrackOption = {
  key: string;
  label: string;
  src: string;
  srcLang: string;
};

function formatClock(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds <= 0) return "0:00";
  const wholeSeconds = Math.floor(totalSeconds);
  const hours = Math.floor(wholeSeconds / 3600);
  const minutes = Math.floor((wholeSeconds % 3600) / 60);
  const seconds = wholeSeconds % 60;
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  }
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function getSeasonEpisodeLabel(item: MediaItem): string | null {
  const season = item.season ?? 0;
  const episode = item.episode ?? 0;
  if (season <= 0 && episode <= 0) return null;
  return `S${String(season).padStart(2, "0")}E${String(episode).padStart(2, "0")}`;
}

function getVideoMetadata(item: MediaItem): string {
  const bits = [item.type === "movie" ? "Movie" : item.type === "anime" ? "Anime" : "TV"];
  const seasonEpisode = getSeasonEpisodeLabel(item);
  const releaseYear =
    item.release_date?.split("-")[0] ||
    (item.type === "movie" ? item.title.match(/\((\d{4})\)$/)?.[1] : undefined);

  if (seasonEpisode) bits.push(seasonEpisode);
  if (releaseYear) bits.push(releaseYear);
  if (item.duration > 0) bits.push(formatClock(item.duration));
  return bits.join(" • ");
}

function getMusicMetadata(item: MediaItem, queueIndex: number, queueSize: number): string {
  const bits = [item.artist || "Unknown Artist"];
  if (item.album) bits.push(item.album);
  if (queueSize > 0) bits.push(`${queueIndex + 1}/${queueSize}`);
  return bits.join(" • ");
}

const CONTROLS_HIDE_DELAY = 3000;

/* ── Subtitle popover menu (shared between docked & fullscreen) ── */
function SubtitleMenu({
  tracks,
  selectedKey,
  onSelect,
  menuRef,
  position = "above",
}: {
  tracks: SubtitleTrackOption[];
  selectedKey: string;
  onSelect: (key: string) => void;
  menuRef: RefObject<HTMLDivElement | null>;
  position?: "above" | "below";
}) {
  return (
    <div
      ref={menuRef}
      className={`subtitle-menu subtitle-menu--${position}`}
      role="listbox"
      aria-label="Select subtitle track"
    >
      <button
        type="button"
        role="option"
        aria-selected={selectedKey === "off"}
        className={`subtitle-menu__item${selectedKey === "off" ? " is-selected" : ""}`}
        onClick={() => onSelect("off")}
      >
        <span className="subtitle-menu__check">{selectedKey === "off" ? "✓" : ""}</span>
        <span>Off</span>
      </button>
      {tracks.map((track) => (
        <button
          key={track.key}
          type="button"
          role="option"
          aria-selected={selectedKey === track.key}
          className={`subtitle-menu__item${selectedKey === track.key ? " is-selected" : ""}`}
          onClick={() => onSelect(track.key)}
        >
          <span className="subtitle-menu__check">{selectedKey === track.key ? "✓" : ""}</span>
          <span>{track.label}</span>
        </button>
      ))}
    </div>
  );
}

export function PlaybackDock() {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [playbackState, setPlaybackState] = useState<PlaybackState>({
    currentTime: 0,
    duration: 0,
    isPlaying: false,
  });
  const [selectedSubtitleKey, setSelectedSubtitleKey] = useState("off");
  const [subtitleTrackVersion, setSubtitleTrackVersion] = useState(0);
  const [subtitleMenuOpen, setSubtitleMenuOpen] = useState(false);
  const subtitleMenuRef = useRef<HTMLDivElement | null>(null);
  const subtitleBtnRef = useRef<HTMLButtonElement | null>(null);
  const [controlsVisible, setControlsVisible] = useState(true);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout>>(0);
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const {
    activeItem,
    activeMode,
    isDockOpen,
    viewMode,
    queue,
    queueIndex,
    shuffle,
    repeatMode,
    volume,
    muted,
    wsConnected,
    lastEvent,
    registerMediaElement,
    togglePlayPause,
    seekTo,
    setMuted,
    setVolume,
    enterFullscreen,
    exitFullscreen,
    dismissDock,
    playNextInQueue,
    playPreviousInQueue,
    toggleShuffle,
    cycleRepeatMode,
  } = usePlayer();

  const isVideo = activeMode === "video" && activeItem != null;
  const isMusic = activeMode === "music" && activeItem != null;
  const isFullscreen = isVideo && viewMode === "fullscreen";

  const subtitleTracks = useMemo(() => {
    if (!isVideo || !activeItem) return [];
    const external =
      activeItem.subtitles?.map((subtitle, index) => ({
        key: `ext-${subtitle.id}`,
        label: subtitle.title || subtitle.language || `Subtitle ${index + 1}`,
        src: externalSubtitleUrl(BASE_URL, subtitle.id),
        srcLang: subtitle.language || "und",
      })) ?? [];
    const embedded =
      activeItem.embeddedSubtitles?.map((subtitle, index) => ({
        key: `emb-${subtitle.streamIndex}`,
        label: subtitle.title || subtitle.language || `Embedded subtitle ${index + 1}`,
        src: embeddedSubtitleUrl(BASE_URL, activeItem.id, subtitle.streamIndex),
        srcLang: subtitle.language || "und",
      })) ?? [];
    return [...external, ...embedded];
  }, [activeItem, isVideo]);

  const selectedSubtitleIndex = useMemo(
    () => subtitleTracks.findIndex((track) => track.key === selectedSubtitleKey),
    [selectedSubtitleKey, subtitleTracks],
  );

  const syncPlaybackState = useCallback(
    (element: HTMLMediaElement | null) => {
      if (!element) {
        setPlaybackState({ currentTime: 0, duration: 0, isPlaying: false });
        return;
      }
      setPlaybackState({
        currentTime: Number.isFinite(element.currentTime) ? element.currentTime : 0,
        duration:
          Number.isFinite(element.duration) && element.duration > 0
            ? element.duration
            : (activeItem?.duration ?? 0),
        isPlaying: !element.paused && !element.ended,
      });
    },
    [activeItem?.duration],
  );

  const setVideoRef = useCallback(
    (element: HTMLVideoElement | null) => {
      videoRef.current = element;
      registerMediaElement("video", element);
      syncPlaybackState(element);
    },
    [registerMediaElement, syncPlaybackState],
  );

  const setAudioRef = useCallback(
    (element: HTMLAudioElement | null) => {
      audioRef.current = element;
      registerMediaElement("audio", element);
      syncPlaybackState(element);
    },
    [registerMediaElement, syncPlaybackState],
  );

  useEffect(() => {
    setPlaybackState({
      currentTime: 0,
      duration: activeItem?.duration ?? 0,
      isPlaying: false,
    });
    setSelectedSubtitleKey("off");
    setSubtitleTrackVersion(0);
  }, [activeItem?.id, activeItem?.duration]);

  /* ── Subtitle track activation ── */
  const activateSubtitleTrack = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    for (let i = 0; i < video.textTracks.length; i += 1) {
      const tt = video.textTracks[i];
      if (!tt) continue;
      tt.mode = i === selectedSubtitleIndex ? "showing" : "disabled";
    }
  }, [selectedSubtitleIndex]);

  useEffect(() => {
    activateSubtitleTrack();
  }, [activateSubtitleTrack, subtitleTrackVersion]);

  /* ── Close subtitle menu on outside click ── */
  useEffect(() => {
    if (!subtitleMenuOpen) return;
    const onClick = (e: MouseEvent) => {
      if (
        subtitleMenuRef.current?.contains(e.target as Node) ||
        subtitleBtnRef.current?.contains(e.target as Node)
      )
        return;
      setSubtitleMenuOpen(false);
    };
    document.addEventListener("pointerdown", onClick);
    return () => document.removeEventListener("pointerdown", onClick);
  }, [subtitleMenuOpen]);

  /* ── Auto-hide controls in fullscreen ── */
  const resetHideTimer = useCallback(() => {
    setControlsVisible(true);
    clearTimeout(hideTimerRef.current);
    hideTimerRef.current = setTimeout(() => {
      setControlsVisible(false);
    }, CONTROLS_HIDE_DELAY);
  }, []);

  useEffect(() => {
    if (!isFullscreen) {
      setControlsVisible(true);
      clearTimeout(hideTimerRef.current);
      return;
    }
    resetHideTimer();
    return () => clearTimeout(hideTimerRef.current);
  }, [isFullscreen, resetHideTimer]);

  const handleFullscreenMouseMove = useCallback(() => {
    if (isFullscreen) resetHideTimer();
  }, [isFullscreen, resetHideTimer]);

  const handleOverlayMouseEnter = useCallback(() => {
    clearTimeout(hideTimerRef.current);
    setControlsVisible(true);
  }, []);

  /* ── Keyboard shortcuts (fullscreen) ── */
  useEffect(() => {
    if (!isFullscreen || !isVideo) return;
    const onKeyDown = (event: KeyboardEvent) => {
      /* Ignore when a form element is focused */
      const tag = (event.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return;

      switch (event.key) {
        case "Escape":
          exitFullscreen();
          break;
        case "f":
        case "F":
          exitFullscreen();
          break;
        case " ":
          event.preventDefault();
          togglePlayPause();
          resetHideTimer();
          break;
        case "ArrowLeft":
          event.preventDefault();
          seekTo(Math.max(0, (videoRef.current?.currentTime ?? 0) - 10));
          resetHideTimer();
          break;
        case "ArrowRight":
          event.preventDefault();
          seekTo((videoRef.current?.currentTime ?? 0) + 10);
          resetHideTimer();
          break;
        case "ArrowUp":
          event.preventDefault();
          setVolume(Math.min(1, volume + 0.1));
          resetHideTimer();
          break;
        case "ArrowDown":
          event.preventDefault();
          setVolume(Math.max(0, volume - 0.1));
          resetHideTimer();
          break;
        case "m":
        case "M":
          setMuted(!muted);
          resetHideTimer();
          break;
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    exitFullscreen,
    isFullscreen,
    isVideo,
    muted,
    resetHideTimer,
    seekTo,
    setMuted,
    setVolume,
    togglePlayPause,
    volume,
  ]);

  if (!activeItem || !isDockOpen || !activeMode) {
    return null;
  }

  const posterUrl = activeItem.poster_path ? tmdbPosterUrl(activeItem.poster_path, "w500") : "";
  const backdropUrl = activeItem.backdrop_path
    ? tmdbBackdropUrl(activeItem.backdrop_path, "w780")
    : "";
  const progressMax =
    playbackState.duration > 0 ? playbackState.duration : Math.max(activeItem.duration, 0);
  const repeatLabel =
    repeatMode === "one" ? "Repeat track" : repeatMode === "all" ? "Repeat queue" : "Repeat off";

  /* ── Fullscreen video player ── */
  if (isFullscreen) {
    const seasonEpisode = getSeasonEpisodeLabel(activeItem);
    const titleDisplay = seasonEpisode
      ? `${seasonEpisode} · ${activeItem.title}`
      : activeItem.title;

    return (
      <section
        className={`fullscreen-player${controlsVisible ? "" : " fullscreen-player--hidden"}`}
        aria-label="Fullscreen video player"
        role="button"
        tabIndex={0}
        onMouseMove={handleFullscreenMouseMove}
        onClick={(event) => {
          /* Toggle play/pause on click (but not on controls) */
          if (
            event.target === event.currentTarget ||
            (event.target as HTMLElement).tagName === "VIDEO"
          ) {
            togglePlayPause();
            resetHideTimer();
          }
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            togglePlayPause();
            resetHideTimer();
          }
        }}
      >
        <video
          key={activeItem.id}
          ref={setVideoRef}
          className="fullscreen-player__video"
          src={mediaStreamUrl(BASE_URL, activeItem.id)}
          crossOrigin="use-credentials"
          autoPlay
          playsInline
          onLoadedMetadata={(event) => syncPlaybackState(event.currentTarget)}
          onTimeUpdate={(event) => syncPlaybackState(event.currentTarget)}
          onPlay={(event) => syncPlaybackState(event.currentTarget)}
          onPause={(event) => syncPlaybackState(event.currentTarget)}
          onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
        >
          {subtitleTracks.map((track) => (
            <track
              key={track.key}
              kind="subtitles"
              src={track.src}
              label={track.label}
              srcLang={track.srcLang}
              onLoad={() => setSubtitleTrackVersion((value) => value + 1)}
              onError={() => setSubtitleTrackVersion((value) => value + 1)}
            />
          ))}
        </video>

        {/* Top title bar */}
        <div className="fullscreen-player__top-bar">
          <div className="fullscreen-player__title-area">
            <h2 className="fullscreen-player__title">{titleDisplay}</h2>
            <div className="fullscreen-player__status">
              {wsConnected && lastEvent && (
                <>
                  <span className="status-dot" data-connected={wsConnected} />
                  <span>{lastEvent}</span>
                </>
              )}
            </div>
          </div>
          <button
            type="button"
            className="fullscreen-player__close-btn"
            onClick={exitFullscreen}
            aria-label="Return to docked player"
            title="Return to docked player"
          >
            <Minimize2 className="size-5" />
          </button>
        </div>

        {/* Bottom controls overlay */}
        <div
          ref={overlayRef}
          className="fullscreen-player__controls"
          onMouseEnter={handleOverlayMouseEnter}
        >
          {/* Seek bar full-width */}
          <div className="fullscreen-player__seek">
            <input
              type="range"
              className="fullscreen-player__seek-slider"
              aria-label="Seek playback"
              min={0}
              max={progressMax || 0}
              step={1}
              value={Math.min(playbackState.currentTime, progressMax || 0)}
              onChange={(event) => seekTo(Number(event.target.value))}
            />
          </div>

          <div className="fullscreen-player__controls-row">
            {/* Left: play + time */}
            <div className="fullscreen-player__controls-left">
              <button
                type="button"
                className="fullscreen-player__ctrl-btn"
                onClick={togglePlayPause}
                aria-label={playbackState.isPlaying ? "Pause playback" : "Play playback"}
              >
                {playbackState.isPlaying ? (
                  <Pause className="size-5" />
                ) : (
                  <Play className="size-5" />
                )}
              </button>
              <span className="fullscreen-player__time">
                {formatClock(playbackState.currentTime)} / {formatClock(progressMax)}
              </span>
            </div>

            {/* Right: subtitles + volume + exit */}
            <div className="fullscreen-player__controls-right">
              {subtitleTracks.length > 0 && (
                <div className="fullscreen-player__subtitle-wrap">
                  <button
                    ref={subtitleBtnRef}
                    type="button"
                    className={`fullscreen-player__ctrl-btn${selectedSubtitleKey !== "off" ? " is-active" : ""}`}
                    aria-label="Subtitles"
                    title="Subtitles"
                    onClick={() => setSubtitleMenuOpen((v) => !v)}
                  >
                    <Subtitles className="size-5" />
                  </button>
                  {subtitleMenuOpen && (
                    <SubtitleMenu
                      menuRef={subtitleMenuRef}
                      tracks={subtitleTracks}
                      selectedKey={selectedSubtitleKey}
                      onSelect={(key) => {
                        setSelectedSubtitleKey(key);
                        setSubtitleMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}

              <div className="fullscreen-player__volume-group">
                <button
                  type="button"
                  className="fullscreen-player__ctrl-btn"
                  onClick={() => setMuted(!muted)}
                  aria-label={muted || volume === 0 ? "Unmute" : "Mute"}
                >
                  {muted || volume === 0 ? (
                    <VolumeX className="size-5" />
                  ) : (
                    <Volume2 className="size-5" />
                  )}
                </button>
                <input
                  type="range"
                  className="fullscreen-player__volume-slider"
                  aria-label="Set volume"
                  min={0}
                  max={1}
                  step={0.01}
                  value={muted ? 0 : volume}
                  onChange={(event) => setVolume(Number(event.target.value))}
                />
              </div>

              <button
                type="button"
                className="fullscreen-player__ctrl-btn"
                onClick={exitFullscreen}
                aria-label="Exit fullscreen"
                title="Exit fullscreen"
              >
                <Minimize2 className="size-4" />
              </button>
            </div>
          </div>
        </div>
      </section>
    );
  }

  /* ── Docked player (music + video) ── */
  return (
    <section
      className={`playback-dock playback-dock--${activeMode} playback-dock--${viewMode}`}
      aria-label={isMusic ? "Music player" : "Playback dock"}
    >
      {isVideo && backdropUrl && (
        <div className="playback-dock__backdrop" aria-hidden="true">
          <img src={backdropUrl} alt="" />
        </div>
      )}

      <div className="playback-dock__shell">
        <div className="playback-dock__topbar">
          <div className="playback-dock__status">
            {isVideo && (
              <>
                <span className="status-dot" data-connected={wsConnected} />
                <span className="playback-dock__status-copy">
                  {lastEvent ||
                    (wsConnected ? "Waiting for transcode updates" : "WebSocket disconnected")}
                </span>
              </>
            )}
          </div>
          <div className="playback-dock__actions">
            {isVideo && (
              <button
                type="button"
                className="playback-dock__icon-button"
                onClick={enterFullscreen}
                aria-label="Open fullscreen player"
                title="Open fullscreen player"
              >
                <Expand className="size-4" />
              </button>
            )}
            <button
              type="button"
              className="playback-dock__icon-button"
              onClick={dismissDock}
              aria-label="Close player"
              title="Close player"
            >
              <X className="size-4" />
            </button>
          </div>
        </div>

        <div className="playback-dock__content">
          <div className="playback-dock__summary">
            <div className="playback-dock__artwork">
              {posterUrl ? (
                <img src={posterUrl} alt="" />
              ) : (
                <img src="/placeholder-poster.png" alt="" />
              )}
            </div>
            <div className="playback-dock__copy">
              <div className="playback-dock__eyebrow">
                {isVideo
                  ? getVideoMetadata(activeItem)
                  : getMusicMetadata(activeItem, queueIndex, queue.length)}
              </div>
              <h2 className="playback-dock__title">{activeItem.title}</h2>
              {isMusic && (
                <div className="playback-dock__subcopy">
                  {activeItem.album_artist && activeItem.album_artist !== activeItem.artist
                    ? `Album artist: ${activeItem.album_artist}`
                    : activeItem.release_year
                      ? `Released ${activeItem.release_year}`
                      : "Docked playback"}
                </div>
              )}
              {isVideo && activeItem.overview && (
                <p className="playback-dock__overview">{activeItem.overview}</p>
              )}
              {isVideo && subtitleTracks.length > 0 && (
                <div className="playback-dock__subtitle-picker">
                  <button
                    ref={subtitleBtnRef}
                    type="button"
                    className={`playback-dock__subtitle-btn${selectedSubtitleKey !== "off" ? " is-active" : ""}`}
                    onClick={() => setSubtitleMenuOpen((v) => !v)}
                    aria-label="Subtitles"
                  >
                    <Subtitles className="size-4" />
                    <span>Subtitles</span>
                  </button>
                  {subtitleMenuOpen && (
                    <SubtitleMenu
                      menuRef={subtitleMenuRef}
                      tracks={subtitleTracks}
                      selectedKey={selectedSubtitleKey}
                      position="above"
                      onSelect={(key) => {
                        setSelectedSubtitleKey(key);
                        setSubtitleMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}
            </div>
          </div>

          {isVideo && (
            <button
              type="button"
              className="playback-dock__surface"
              onClick={enterFullscreen}
              aria-label={`Open fullscreen player for ${activeItem.title}`}
            >
              <video
                key={activeItem.id}
                ref={setVideoRef}
                className="playback-dock__video"
                src={mediaStreamUrl(BASE_URL, activeItem.id)}
                crossOrigin="use-credentials"
                autoPlay
                playsInline
                onLoadedMetadata={(event) => syncPlaybackState(event.currentTarget)}
                onTimeUpdate={(event) => syncPlaybackState(event.currentTarget)}
                onPlay={(event) => syncPlaybackState(event.currentTarget)}
                onPause={(event) => syncPlaybackState(event.currentTarget)}
                onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
              >
                {subtitleTracks.map((track) => (
                  <track
                    key={track.key}
                    kind="subtitles"
                    src={track.src}
                    label={track.label}
                    srcLang={track.srcLang}
                    onLoad={() => setSubtitleTrackVersion((value) => value + 1)}
                    onError={() => setSubtitleTrackVersion((value) => value + 1)}
                  />
                ))}
              </video>
              <span className="playback-dock__surface-hint">Click video to expand</span>
            </button>
          )}

          {isMusic && (
            <audio
              key={activeItem.id}
              ref={setAudioRef}
              className="playback-dock__audio"
              src={mediaStreamUrl(BASE_URL, activeItem.id)}
              autoPlay
              onLoadedMetadata={(event) => syncPlaybackState(event.currentTarget)}
              onTimeUpdate={(event) => syncPlaybackState(event.currentTarget)}
              onPlay={(event) => syncPlaybackState(event.currentTarget)}
              onPause={(event) => syncPlaybackState(event.currentTarget)}
              onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
              onEnded={() => {
                if (repeatMode === "one" && audioRef.current) {
                  audioRef.current.currentTime = 0;
                  void audioRef.current.play().catch(() => {});
                  return;
                }
                playNextInQueue();
              }}
            />
          )}
        </div>

        <div className="playback-dock__transport">
          <div className="playback-dock__buttons">
            {isMusic && (
              <>
                <button
                  type="button"
                  className={`playback-dock__icon-button${shuffle ? " is-active" : ""}`}
                  onClick={toggleShuffle}
                  aria-label={shuffle ? "Disable shuffle" : "Enable shuffle"}
                >
                  <Shuffle className="size-4" />
                </button>
                <button
                  type="button"
                  className="playback-dock__icon-button"
                  onClick={playPreviousInQueue}
                  aria-label="Previous track"
                >
                  <SkipBack className="size-4" />
                </button>
              </>
            )}

            <button
              type="button"
              className="playback-dock__play-button"
              onClick={togglePlayPause}
              aria-label={playbackState.isPlaying ? "Pause playback" : "Play playback"}
            >
              {playbackState.isPlaying ? <Pause className="size-5" /> : <Play className="size-5" />}
            </button>

            {isMusic && (
              <>
                <button
                  type="button"
                  className="playback-dock__icon-button"
                  onClick={playNextInQueue}
                  aria-label="Next track"
                >
                  <SkipForward className="size-4" />
                </button>
                <button
                  type="button"
                  className={`playback-dock__icon-button${repeatMode !== "off" ? " is-active" : ""}`}
                  onClick={cycleRepeatMode}
                  aria-label={repeatLabel}
                  title={repeatLabel}
                >
                  <Repeat className="size-4" />
                  <span className="playback-dock__repeat-copy">
                    {repeatMode === "one" ? "1" : repeatMode === "all" ? "all" : "off"}
                  </span>
                </button>
              </>
            )}
          </div>

          <div className="playback-dock__timeline">
            <span className="playback-dock__time">{formatClock(playbackState.currentTime)}</span>
            <input
              type="range"
              className="playback-dock__slider"
              aria-label="Seek playback"
              min={0}
              max={progressMax || 0}
              step={1}
              value={Math.min(playbackState.currentTime, progressMax || 0)}
              onChange={(event) => seekTo(Number(event.target.value))}
            />
            <span className="playback-dock__time">{formatClock(progressMax)}</span>
          </div>

          <div className="playback-dock__volume">
            <button
              type="button"
              className="playback-dock__icon-button"
              onClick={() => setMuted(!muted)}
              aria-label={muted || volume === 0 ? "Unmute" : "Mute"}
            >
              {muted || volume === 0 ? (
                <VolumeX className="size-4" />
              ) : (
                <Volume2 className="size-4" />
              )}
            </button>
            <input
              type="range"
              className="playback-dock__slider playback-dock__slider--volume"
              aria-label="Set volume"
              min={0}
              max={1}
              step={0.01}
              value={muted ? 0 : volume}
              onChange={(event) => setVolume(Number(event.target.value))}
            />
          </div>
        </div>
      </div>
    </section>
  );
}
