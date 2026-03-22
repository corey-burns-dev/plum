import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type RefObject,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import Hls from "hls.js";
import {
  embeddedSubtitleUrl,
  externalSubtitleUrl,
  mediaStreamUrl,
  tmdbBackdropUrl,
  tmdbPosterUrl,
} from "@plum/shared";
import {
  Expand,
  Settings,
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
import { BASE_URL, updateMediaProgress } from "../api";
import { usePlayer } from "../contexts/PlayerContext";
import {
  readStoredSubtitleAppearance,
  readStoredPlayerControlsAppearance,
  resolveLibraryPlaybackPreferences,
  subscribeToPlayerControlsAppearance,
  subtitleFontSizeValue,
  subtitlePositionOptions,
  subtitleSizeOptions,
  playerControlsAppearanceOptions,
  writeStoredSubtitleAppearance,
  writeStoredPlayerControlsAppearance,
  type PlayerControlsAppearance,
  type SubtitleAppearance,
} from "../lib/playbackPreferences";
import {
  applyCueLineSetting,
  bufferedRangeStartsNearZero,
  buildSubtitleCues,
  clearTextTrackCues,
  formatClock,
  formatHlsErrorMessage,
  formatTrackLabel,
  getBrowserAudioTracks,
  getMusicMetadata,
  getPreferredAudioKey,
  getPreferredSubtitleKey,
  getSeasonEpisodeLabel,
  getVideoMetadata,
  hasTextTrack,
  nudgeVideoIntoBufferedRange,
  resolvedVideoDuration,
} from "../lib/playback/playerMedia";
import type {
  AudioTrackOption,
  HlsErrorData,
  SubtitleTrackOption,
  TrackMenuOption,
} from "../lib/playback/playerMedia";
import { queryKeys, useLibraries } from "../queries";

type PlaybackState = {
  currentTime: number;
  duration: number;
  isPlaying: boolean;
};

type LoadedSubtitleTrack = SubtitleTrackOption & {
  body: string;
};

type VideoProgressSnapshot = {
  mediaId: number;
  positionSeconds: number;
  durationSeconds: number;
  shouldResumePlayback: boolean;
  ended: boolean;
};

const CONTROLS_HIDE_DELAY = 3000;

/* ── Track popover menu (shared between docked & fullscreen) ── */
function TrackMenu({
  options,
  selectedKey,
  onSelect,
  menuRef,
  position = "above",
  ariaLabel,
  offLabel,
}: {
  options: TrackMenuOption[];
  selectedKey: string;
  onSelect: (key: string) => void;
  menuRef: RefObject<HTMLDivElement | null>;
  position?: "above" | "below";
  ariaLabel: string;
  offLabel?: string;
}) {
  return (
    <div
      ref={menuRef}
      className={`subtitle-menu subtitle-menu--${position}`}
      role="listbox"
      aria-label={ariaLabel}
    >
      {offLabel && (
        <button
          type="button"
          role="option"
          aria-selected={selectedKey === "off"}
          className={`subtitle-menu__item${selectedKey === "off" ? " is-selected" : ""}`}
          onClick={() => onSelect("off")}
        >
          <span className="subtitle-menu__check">{selectedKey === "off" ? "✓" : ""}</span>
          <span>{offLabel}</span>
        </button>
      )}
      {options.map((option) => (
        <button
          key={option.key}
          type="button"
          role="option"
          aria-selected={selectedKey === option.key}
          className={`subtitle-menu__item${selectedKey === option.key ? " is-selected" : ""}`}
          onClick={() => onSelect(option.key)}
        >
          <span className="subtitle-menu__check">{selectedKey === option.key ? "✓" : ""}</span>
          <span>{option.label}</span>
        </button>
      ))}
    </div>
  );
}

function PlayerSettingsMenu({
  menuRef,
  preferences,
  controlsAppearance,
  onChange,
  onControlsAppearanceChange,
}: {
  menuRef: RefObject<HTMLDivElement | null>;
  preferences: SubtitleAppearance;
  controlsAppearance: PlayerControlsAppearance;
  onChange: (value: SubtitleAppearance) => void;
  onControlsAppearanceChange: (value: PlayerControlsAppearance) => void;
}) {
  return (
    <div
      ref={menuRef}
      className="player-settings-menu"
      role="dialog"
      aria-label="Subtitle settings"
    >
      <label className="player-settings-menu__field">
        <span>Subtitle size</span>
        <select
          value={preferences.size}
          onChange={(event) =>
            onChange({
              ...preferences,
              size: event.target.value as SubtitleAppearance["size"],
            })
          }
        >
          {subtitleSizeOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>

      <label className="player-settings-menu__field">
        <span>Subtitle location</span>
        <select
          value={preferences.position}
          onChange={(event) =>
            onChange({
              ...preferences,
              position: event.target.value as SubtitleAppearance["position"],
            })
          }
        >
          {subtitlePositionOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>

      <label className="player-settings-menu__field">
        <span>Subtitle color</span>
        <input
          type="color"
          value={preferences.color}
          onChange={(event) =>
            onChange({
              ...preferences,
              color: event.target.value,
            })
          }
        />
      </label>

      <div className="player-settings-menu__field">
        <span>Controls look</span>
        <div
          className="player-settings-menu__choice-row"
          role="group"
          aria-label="Player controls look"
        >
          {playerControlsAppearanceOptions.map((option) => (
            <button
              key={option.value}
              type="button"
              className={`player-settings-menu__choice${controlsAppearance === option.value ? " is-active" : ""}`}
              onClick={() => onControlsAppearanceChange(option.value)}
              aria-pressed={controlsAppearance === option.value}
              title={option.description}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

export function PlaybackDock() {
  const queryClient = useQueryClient();
  const { data: libraries = [], isFetched: librariesFetched } = useLibraries();
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const playerRootRef = useRef<HTMLElement | null>(null);
  const lastPersistedRef = useRef<{
    mediaId: number;
    positionSeconds: number;
    completed: boolean;
  } | null>(null);
  const initialProgressPersistedRef = useRef<number | null>(null);
  const resumeAppliedRef = useRef<number | null>(null);
  const defaultTrackSelectionAppliedRef = useRef<number | null>(null);
  const [playbackState, setPlaybackState] = useState<PlaybackState>({
    currentTime: 0,
    duration: 0,
    isPlaying: false,
  });
  const [subtitleAppearance, setSubtitleAppearance] = useState<SubtitleAppearance>(() =>
    readStoredSubtitleAppearance(),
  );
  const [playerControlsAppearance, setPlayerControlsAppearance] =
    useState<PlayerControlsAppearance>(() => readStoredPlayerControlsAppearance());
  const [selectedSubtitleKey, setSelectedSubtitleKey] = useState("off");
  const [loadedSubtitleTracks, setLoadedSubtitleTracks] = useState<LoadedSubtitleTrack[]>([]);
  const [failedSubtitleKeys, setFailedSubtitleKeys] = useState<string[]>([]);
  const [selectedAudioKey, setSelectedAudioKey] = useState("");
  const [audioTrackVersion, setAudioTrackVersion] = useState(0);
  const [videoAttachmentVersion, setVideoAttachmentVersion] = useState(0);
  const [subtitleAttachmentVersion, setSubtitleAttachmentVersion] = useState(0);
  const [subtitleReadyVersion, setSubtitleReadyVersion] = useState(0);
  const [subtitleMenuOpen, setSubtitleMenuOpen] = useState(false);
  const [audioMenuOpen, setAudioMenuOpen] = useState(false);
  const [playerSettingsOpen, setPlayerSettingsOpen] = useState(false);
  const [browserFullscreenActive, setBrowserFullscreenActive] = useState(false);
  const [pendingBrowserFullscreen, setPendingBrowserFullscreen] = useState(false);
  const subtitleMenuRef = useRef<HTMLDivElement | null>(null);
  const subtitleBtnRef = useRef<HTMLButtonElement | null>(null);
  const audioMenuRef = useRef<HTMLDivElement | null>(null);
  const audioBtnRef = useRef<HTMLButtonElement | null>(null);
  const playerSettingsMenuRef = useRef<HTMLDivElement | null>(null);
  const playerSettingsBtnRef = useRef<HTMLButtonElement | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const requestedAudioTrackRef = useRef<{ mediaId: number; key: string } | null>(null);
  const dispatchedAudioTrackRef = useRef<{ mediaId: number; key: string } | null>(null);
  const [controlsVisible, setControlsVisible] = useState(true);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout>>(0);
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const seekToAfterReloadRef = useRef<number | null>(null);
  const resumePlaybackAfterReloadRef = useRef(false);
  const previousVideoSourceUrlRef = useRef("");
  const [hlsStatusMessage, setHlsStatusMessage] = useState("");
  const mediaRecoveryAttemptsRef = useRef(0);
  const networkRecoveryAttemptsRef = useRef(0);
  const initialBufferGapHandledRef = useRef(false);
  const manualSubtitleTrackRef = useRef<TextTrack | null>(null);
  const manualSubtitleVideoRef = useRef<HTMLVideoElement | null>(null);
  const subtitleLoadControllersRef = useRef<Map<string, AbortController>>(new Map());
  const lastVideoProgressRef = useRef<VideoProgressSnapshot | null>(null);
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
    videoSourceUrl,
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
    changeAudioTrack,
  } = usePlayer();
  const registerMediaElementRef = useRef(registerMediaElement);

  const isVideo = activeMode === "video" && activeItem != null;
  const isMusic = activeMode === "music" && activeItem != null;
  const isFullscreen = isVideo && viewMode === "fullscreen";
  const activeItemId = activeItem?.id ?? null;
  const activeItemDuration = activeItem?.duration ?? 0;
  const videoStatusMessage =
    hlsStatusMessage ||
    lastEvent ||
    (wsConnected ? "Waiting for transcode updates" : "WebSocket disconnected");
  const activeLibrary = useMemo(
    () => libraries.find((library) => library.id === activeItem?.library_id) ?? null,
    [activeItem?.library_id, libraries],
  );
  const libraryPlaybackPreferences = useMemo(
    () =>
      resolveLibraryPlaybackPreferences(
        activeLibrary ?? (activeItem ? { type: activeItem.type } : null),
      ),
    [activeItem, activeLibrary],
  );

  useEffect(() => {
    const previousUrl = previousVideoSourceUrlRef.current;
    previousVideoSourceUrlRef.current = videoSourceUrl;
    const sourceChanged = previousUrl !== videoSourceUrl;
    if (sourceChanged) {
      setHlsStatusMessage("");
      mediaRecoveryAttemptsRef.current = 0;
      networkRecoveryAttemptsRef.current = 0;
      initialBufferGapHandledRef.current = false;
      setSubtitleReadyVersion(0);
    }
    if (!videoSourceUrl || !previousUrl || !sourceChanged) return;
    const video = videoRef.current;
    if (!video) return;
    seekToAfterReloadRef.current =
      Number.isFinite(video.currentTime) && video.currentTime > 0
        ? video.currentTime
        : playbackState.currentTime;
    resumePlaybackAfterReloadRef.current = !video.paused && !video.ended;
    video.pause();
    video.load();
  }, [playbackState.currentTime, videoSourceUrl]);

  const subtitleTrackRequests = useMemo<SubtitleTrackOption[]>(() => {
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

  const failedSubtitleKeySet = useMemo(() => new Set(failedSubtitleKeys), [failedSubtitleKeys]);
  const subtitleTrackOptions = useMemo(
    () => subtitleTrackRequests.filter((track) => !failedSubtitleKeySet.has(track.key)),
    [failedSubtitleKeySet, subtitleTrackRequests],
  );
  const ensureSubtitleTrackLoaded = useCallback(
    async (trackKey: string) => {
      if (trackKey === "off") return;
      if (loadedSubtitleTracks.some((track) => track.key === trackKey)) return;
      if (failedSubtitleKeySet.has(trackKey)) return;
      if (subtitleLoadControllersRef.current.has(trackKey)) return;
      const track = subtitleTrackRequests.find((candidate) => candidate.key === trackKey);
      if (!track) return;

      const controller = new AbortController();
      subtitleLoadControllersRef.current.set(trackKey, controller);

      try {
        const response = await fetch(track.src, {
          credentials: "include",
          signal: controller.signal,
        });
        if (!response.ok) {
          throw new Error(`Subtitle request failed: ${response.status}`);
        }
        const body = await response.text();
        setLoadedSubtitleTracks((current) =>
          current.some((candidate) => candidate.key === track.key)
            ? current
            : [...current, { ...track, body }],
        );
      } catch (error) {
        if (
          (error instanceof DOMException && error.name === "AbortError") ||
          controller.signal.aborted
        ) {
          return;
        }
        console.error("[PlaybackDock] Subtitle load failed", {
          mediaId: activeItem?.id ?? null,
          source: track.src,
          error,
        });
        setFailedSubtitleKeys((current) =>
          current.includes(track.key) ? current : [...current, track.key],
        );
        setLoadedSubtitleTracks((current) =>
          current.filter((candidate) => candidate.key !== track.key),
        );
        setSelectedSubtitleKey((current) => (current === track.key ? "off" : current));
      } finally {
        subtitleLoadControllersRef.current.delete(trackKey);
      }
    },
    [activeItem?.id, failedSubtitleKeySet, loadedSubtitleTracks, subtitleTrackRequests],
  );

  useEffect(() => {
    if (selectedSubtitleKey === "off") return;
    void ensureSubtitleTrackLoaded(selectedSubtitleKey);
  }, [ensureSubtitleTrackLoaded, selectedSubtitleKey]);

  const audioTracks = useMemo<AudioTrackOption[]>(() => {
    if (!isVideo || !activeItem) return [];
    return (
      activeItem.embeddedAudioTracks?.map((track, index) => ({
        key: `aud-${track.streamIndex}`,
        label: formatTrackLabel(track.title, track.language, `Audio ${index + 1}`),
        streamIndex: track.streamIndex,
        language: track.language,
      })) ?? []
    );
  }, [activeItem, isVideo]);

  const selectedAudioIndex = useMemo(
    () => audioTracks.findIndex((track) => track.key === selectedAudioKey),
    [audioTracks, selectedAudioKey],
  );

  const selectedAudioLabel =
    (selectedAudioIndex >= 0 ? audioTracks[selectedAudioIndex]?.label : audioTracks[0]?.label) ||
    "Audio";
  const videoSubtitleStyle = useMemo(
    () =>
      ({
        "--plum-subtitle-color": subtitleAppearance.color,
        "--plum-subtitle-size": subtitleFontSizeValue(subtitleAppearance.size),
      }) as CSSProperties,
    [subtitleAppearance.color, subtitleAppearance.size],
  );

  const syncPlaybackState = useCallback(
    (element: HTMLMediaElement | null) => {
      if (!element) {
        setPlaybackState({ currentTime: 0, duration: 0, isPlaying: false });
        return;
      }
      const elementDuration =
        Number.isFinite(element.duration) && element.duration > 0 ? element.duration : 0;
      setPlaybackState({
        currentTime: Number.isFinite(element.currentTime) ? element.currentTime : 0,
        duration: isVideo
          ? resolvedVideoDuration(activeItem?.duration ?? 0, elementDuration)
          : elementDuration,
        isPlaying: !element.paused && !element.ended,
      });
    },
    [activeItem?.duration, isVideo],
  );
  const syncPlaybackStateRef = useRef(syncPlaybackState);

  useEffect(() => {
    registerMediaElementRef.current = registerMediaElement;
  }, [registerMediaElement]);

  useEffect(() => {
    syncPlaybackStateRef.current = syncPlaybackState;
  }, [syncPlaybackState]);

  const markSubtitleReady = useCallback(() => {
    setSubtitleReadyVersion((value) => value + 1);
  }, []);

  const maybeRecoverInitialBufferGap = useCallback((video: HTMLVideoElement | null): boolean => {
    if (!video || initialBufferGapHandledRef.current) {
      return false;
    }

    if ((Number.isFinite(video.currentTime) ? video.currentTime : 0) > 1) {
      initialBufferGapHandledRef.current = true;
      return false;
    }

    if (bufferedRangeStartsNearZero(video)) {
      initialBufferGapHandledRef.current = true;
      return false;
    }

    const nudged = nudgeVideoIntoBufferedRange(video);
    if (nudged || video.buffered.length > 0) {
      initialBufferGapHandledRef.current = true;
    }
    return nudged;
  }, []);

  const captureVideoProgressSnapshot = useCallback(
    (element?: HTMLVideoElement | null): VideoProgressSnapshot | null => {
      if (!isVideo || !activeItem) return null;
      const candidate = element ?? videoRef.current;
      const fallback = lastVideoProgressRef.current;
      const fallbackDuration =
        fallback?.mediaId === activeItem.id ? fallback.durationSeconds : 0;
      const fallbackPosition =
        fallback?.mediaId === activeItem.id ? fallback.positionSeconds : playbackState.currentTime;
      const duration = resolvedVideoDuration(
        activeItem.duration,
        candidate?.duration ?? fallbackDuration,
      );
      if (!Number.isFinite(duration) || duration <= 0) return null;
      const rawPosition =
        candidate && Number.isFinite(candidate.currentTime) ? candidate.currentTime : fallbackPosition;
      const positionSeconds = Math.max(0, Math.min(rawPosition, duration));
      const ended = candidate?.ended ?? (fallback?.mediaId === activeItem.id ? fallback.ended : false);
      return {
        mediaId: activeItem.id,
        positionSeconds,
        durationSeconds: duration,
        shouldResumePlayback:
          candidate != null
            ? !candidate.paused && !candidate.ended
            : (fallback?.mediaId === activeItem.id ? fallback.shouldResumePlayback : false),
        ended,
      };
    },
    [activeItem, isVideo, playbackState.currentTime],
  );

  const syncVideoProgressSnapshot = useCallback(
    (element: HTMLVideoElement | null) => {
      const snapshot = captureVideoProgressSnapshot(element);
      if (!snapshot) return;
      lastVideoProgressRef.current = snapshot;
    },
    [captureVideoProgressSnapshot],
  );

  const primeVideoHandoff = useCallback(() => {
    const snapshot = captureVideoProgressSnapshot(videoRef.current);
    if (!snapshot) return null;
    lastVideoProgressRef.current = snapshot;
    seekToAfterReloadRef.current = snapshot.positionSeconds;
    resumePlaybackAfterReloadRef.current = snapshot.shouldResumePlayback;
    return snapshot;
  }, [captureVideoProgressSnapshot]);

  const persistPlaybackProgress = useCallback(
    async (options?: { force?: boolean; completed?: boolean; snapshot?: VideoProgressSnapshot | null }) => {
      if (!isVideo || !activeItem) return;
      const snapshot =
        options?.snapshot && options.snapshot.mediaId === activeItem.id
          ? options.snapshot
          : captureVideoProgressSnapshot();
      if (!snapshot) return;
      lastVideoProgressRef.current = snapshot;
      const completed = options?.completed === true || snapshot.ended;
      const previous = lastPersistedRef.current;
      if (
        !options?.force &&
        previous?.mediaId === activeItem.id &&
        previous.completed === completed &&
        Math.abs(previous.positionSeconds - snapshot.positionSeconds) < 10
      ) {
        return;
      }
      await updateMediaProgress(activeItem.id, {
        position_seconds: snapshot.positionSeconds,
        duration_seconds: snapshot.durationSeconds,
        completed,
      }).catch(() => {});
      lastPersistedRef.current = {
        mediaId: activeItem.id,
        positionSeconds: snapshot.positionSeconds,
        completed,
      };
      if (activeItem.library_id != null) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.library(activeItem.library_id) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.home });
    },
    [activeItem, captureVideoProgressSnapshot, isVideo, queryClient],
  );
  const captureVideoProgressSnapshotRef = useRef(captureVideoProgressSnapshot);
  const syncVideoProgressSnapshotRef = useRef(syncVideoProgressSnapshot);
  const persistPlaybackProgressRef = useRef(persistPlaybackProgress);

  useEffect(() => {
    captureVideoProgressSnapshotRef.current = captureVideoProgressSnapshot;
  }, [captureVideoProgressSnapshot]);

  useEffect(() => {
    syncVideoProgressSnapshotRef.current = syncVideoProgressSnapshot;
  }, [syncVideoProgressSnapshot]);

  useEffect(() => {
    persistPlaybackProgressRef.current = persistPlaybackProgress;
  }, [persistPlaybackProgress]);

  const applyResumePosition = useCallback(
    (element: HTMLMediaElement) => {
      if (
        !isVideo ||
        !activeItem ||
        activeItem.completed ||
        resumeAppliedRef.current === activeItem.id
      ) {
        return;
      }
      const resumeAt = activeItem.progress_seconds ?? 0;
      if (!Number.isFinite(resumeAt) || resumeAt <= 0) {
        resumeAppliedRef.current = activeItem.id;
        return;
      }
      const maxResumeTime = resolvedVideoDuration(activeItem.duration, element.duration) - 1;
      element.currentTime = Math.max(0, Math.min(resumeAt, maxResumeTime));
      resumeAppliedRef.current = activeItem.id;
    },
    [activeItem, isVideo],
  );

  const persistInitialPlaybackProgress = useCallback(
    (element: HTMLVideoElement) => {
      if (!isVideo || !activeItem) return;
      if (initialProgressPersistedRef.current === activeItem.id) return;
      if (!Number.isFinite(element.currentTime) || element.currentTime <= 0) return;
      initialProgressPersistedRef.current = activeItem.id;
      const snapshot = captureVideoProgressSnapshot(element);
      void persistPlaybackProgress({ force: true, snapshot });
    },
    [activeItem, captureVideoProgressSnapshot, isVideo, persistPlaybackProgress],
  );

  const setVideoRef = useCallback((element: HTMLVideoElement | null) => {
    if (videoRef.current !== element) {
      if (videoRef.current && !element) {
        const snapshot = captureVideoProgressSnapshotRef.current(videoRef.current);
        if (snapshot) {
          lastVideoProgressRef.current = snapshot;
          seekToAfterReloadRef.current = snapshot.positionSeconds;
          resumePlaybackAfterReloadRef.current = snapshot.shouldResumePlayback;
          if (snapshot.positionSeconds > 0 || snapshot.ended) {
            void persistPlaybackProgressRef.current({ force: true, snapshot });
          }
        }
      }
      manualSubtitleTrackRef.current = null;
      manualSubtitleVideoRef.current = null;
      setVideoAttachmentVersion((value) => value + 1);
      setSubtitleAttachmentVersion((value) => value + 1);
    }
    videoRef.current = element;
    registerMediaElementRef.current("video", element);
    syncPlaybackStateRef.current(element);
    if (element) {
      syncVideoProgressSnapshotRef.current(element);
    }
  }, []);

  const setAudioRef = useCallback((element: HTMLAudioElement | null) => {
    audioRef.current = element;
    registerMediaElementRef.current("audio", element);
    syncPlaybackStateRef.current(element);
  }, []);

  const handleVideoLoadedMetadata = useCallback(
    (element: HTMLVideoElement) => {
      const seekToAfterReload = seekToAfterReloadRef.current;
      if (seekToAfterReload != null) {
        element.currentTime = seekToAfterReload;
        seekToAfterReloadRef.current = null;
        const shouldResumePlayback = resumePlaybackAfterReloadRef.current;
        resumePlaybackAfterReloadRef.current = false;
        if (shouldResumePlayback) {
          void element.play().catch(() => {});
        } else {
          element.pause();
        }
      } else {
        applyResumePosition(element);
      }
      syncPlaybackState(element);
      setAudioTrackVersion((value) => value + 1);
      markSubtitleReady();
    },
    [applyResumePosition, markSubtitleReady, syncPlaybackState],
  );

  useEffect(() => {
    setPlaybackState({
      currentTime: 0,
      duration: activeItemDuration,
      isPlaying: false,
    });
    setSelectedSubtitleKey("off");
    for (const controller of subtitleLoadControllersRef.current.values()) {
      controller.abort();
    }
    subtitleLoadControllersRef.current.clear();
    setLoadedSubtitleTracks([]);
    setFailedSubtitleKeys([]);
    setSelectedAudioKey("");
    setAudioTrackVersion(0);
    setVideoAttachmentVersion(0);
    setSubtitleAttachmentVersion(0);
    setSubtitleReadyVersion(0);
    initialProgressPersistedRef.current = null;
    resumeAppliedRef.current = null;
    defaultTrackSelectionAppliedRef.current = null;
    lastVideoProgressRef.current = null;
    requestedAudioTrackRef.current =
      activeItemId != null ? { mediaId: activeItemId, key: "" } : null;
    dispatchedAudioTrackRef.current = null;
    seekToAfterReloadRef.current = null;
    resumePlaybackAfterReloadRef.current = false;
    previousVideoSourceUrlRef.current = "";
    setHlsStatusMessage("");
    mediaRecoveryAttemptsRef.current = 0;
    networkRecoveryAttemptsRef.current = 0;
    initialBufferGapHandledRef.current = false;
    setSubtitleMenuOpen(false);
    setAudioMenuOpen(false);
    setPlayerSettingsOpen(false);
  }, [activeItemDuration, activeItemId]);

  useEffect(() => {
    if (!activeItem) {
      requestedAudioTrackRef.current = null;
      dispatchedAudioTrackRef.current = null;
      return;
    }
    if (requestedAudioTrackRef.current?.mediaId !== activeItem.id) {
      requestedAudioTrackRef.current = null;
    }
    if (dispatchedAudioTrackRef.current?.mediaId !== activeItem.id) {
      dispatchedAudioTrackRef.current = null;
    }
  }, [activeItem]);

  useEffect(() => {
    if (!isVideo || !activeItem) return;
    const intervalId = window.setInterval(() => {
      void persistPlaybackProgress();
    }, 10_000);
    return () => window.clearInterval(intervalId);
  }, [activeItem, isVideo, persistPlaybackProgress]);

  useEffect(() => {
    writeStoredSubtitleAppearance(subtitleAppearance);
  }, [subtitleAppearance]);

  useEffect(() => {
    writeStoredPlayerControlsAppearance(playerControlsAppearance);
  }, [playerControlsAppearance]);

  useEffect(
    () =>
      subscribeToPlayerControlsAppearance((preference) => {
        setPlayerControlsAppearance((current) => (current === preference ? current : preference));
      }),
    [],
  );

  useEffect(() => {
    if (!isVideo || !activeItem) return;
    if (defaultTrackSelectionAppliedRef.current === activeItem.id) return;
    if (activeItem.library_id != null && !librariesFetched) return;
    setSelectedSubtitleKey(
      getPreferredSubtitleKey(
        subtitleTrackOptions,
        libraryPlaybackPreferences.preferredSubtitleLanguage,
        libraryPlaybackPreferences.subtitlesEnabledByDefault,
      ),
    );
    setSelectedAudioKey(
      getPreferredAudioKey(audioTracks, libraryPlaybackPreferences.preferredAudioLanguage),
    );
    defaultTrackSelectionAppliedRef.current = activeItem.id;
  }, [
    activeItem,
    audioTracks,
    isVideo,
    librariesFetched,
    libraryPlaybackPreferences.preferredAudioLanguage,
    libraryPlaybackPreferences.preferredSubtitleLanguage,
    libraryPlaybackPreferences.subtitlesEnabledByDefault,
    subtitleTrackOptions,
  ]);

  useEffect(
    () => () => {
      void persistPlaybackProgress({ force: true });
    },
    [persistPlaybackProgress],
  );

  useEffect(() => {
    if (!isVideo || !activeItem) return;
    const persist = () => {
      void persistPlaybackProgress({ force: true });
    };
    const onVisibilityChange = () => {
      if (document.visibilityState === "hidden") persist();
    };
    window.addEventListener("pagehide", persist);
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      window.removeEventListener("pagehide", persist);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [activeItem, isVideo, persistPlaybackProgress]);

  const applyManagedSubtitleTrack = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    const hasLoadedSubtitles = loadedSubtitleTracks.length > 0;
    const hasSelectedSubtitle = selectedSubtitleKey !== "off";

    if (!hasSelectedSubtitle && !hasLoadedSubtitles) {
      return;
    }

    let track = manualSubtitleTrackRef.current;
    if (manualSubtitleVideoRef.current !== video || track == null || !hasTextTrack(video, track)) {
      try {
        track = video.addTextTrack("subtitles", "Plum subtitles", "und");
      } catch {
        return;
      }
      if (!track) {
        return;
      }
      manualSubtitleTrackRef.current = track;
      manualSubtitleVideoRef.current = video;
    }

    clearTextTrackCues(track);

    if (selectedSubtitleKey === "off") {
      track.mode = "disabled";
      return;
    }

    const selectedTrack =
      loadedSubtitleTracks.find((candidate) => candidate.key === selectedSubtitleKey) ?? null;
    if (!selectedTrack) {
      track.mode = "disabled";
      return;
    }

    for (const cue of buildSubtitleCues(selectedTrack.body)) {
      applyCueLineSetting(cue, subtitleAppearance.position);
      track.addCue(cue);
    }
    track.mode = "showing";
  }, [loadedSubtitleTracks, selectedSubtitleKey, subtitleAppearance.position]);

  useEffect(() => {
    applyManagedSubtitleTrack();
    return () => {
      clearTextTrackCues(manualSubtitleTrackRef.current);
      if (manualSubtitleTrackRef.current) {
        manualSubtitleTrackRef.current.mode = "disabled";
      }
    };
  }, [applyManagedSubtitleTrack, subtitleAttachmentVersion, subtitleReadyVersion]);

  const syncBrowserAudioTrackSelection = useCallback(() => {
    const browserAudioTracks = getBrowserAudioTracks(videoRef.current);
    if (
      browserAudioTracks == null ||
      browserAudioTracks.length <= 1 ||
      browserAudioTracks.length !== audioTracks.length
    ) {
      return;
    }

    const detectedIndex = Array.from(
      { length: browserAudioTracks.length },
      (_, index) => index,
    ).find((index) => browserAudioTracks[index]?.enabled);
    const activeIndex =
      selectedAudioIndex >= 0 ? selectedAudioIndex : Math.max(0, detectedIndex ?? 0);

    for (let i = 0; i < browserAudioTracks.length; i += 1) {
      const audioTrack = browserAudioTracks[i];
      if (!audioTrack) continue;
      audioTrack.enabled = i === activeIndex;
    }
  }, [audioTracks, selectedAudioIndex]);

  const requestAudioTrackChange = useCallback(
    (key: string) => {
      if (!isVideo || !activeItem || !key) return;
      const track = audioTracks.find((candidate) => candidate.key === key);
      if (!track) return;
      const previousRequest = dispatchedAudioTrackRef.current;
      if (previousRequest?.mediaId === activeItem.id && previousRequest.key === key) return;
      dispatchedAudioTrackRef.current = { mediaId: activeItem.id, key };
      void changeAudioTrack(track.streamIndex);
    },
    [activeItem, audioTracks, changeAudioTrack, isVideo],
  );

  useEffect(() => {
    syncBrowserAudioTrackSelection();
  }, [audioTrackVersion, syncBrowserAudioTrackSelection]);

  useEffect(() => {
    if (!selectedAudioKey) return;
    if (!videoSourceUrl) return;
    syncBrowserAudioTrackSelection();
    if (
      requestedAudioTrackRef.current?.mediaId !== activeItem?.id ||
      requestedAudioTrackRef.current?.key !== selectedAudioKey
    ) {
      return;
    }
    requestAudioTrackChange(selectedAudioKey);
  }, [
    activeItem?.id,
    requestAudioTrackChange,
    selectedAudioKey,
    syncBrowserAudioTrackSelection,
    videoSourceUrl,
  ]);

  useEffect(() => {
    if (hlsRef.current != null) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }

    const video = videoRef.current;
    if (!isVideo || !video) return;

    if (!videoSourceUrl) {
      video.removeAttribute("src");
      video.load();
      return;
    }

    if (!Hls.isSupported()) {
      video.src = videoSourceUrl;
      return;
    }

    const hls = new Hls({
      enableWorker: true,
      backBufferLength: 90,
      startPosition: seekToAfterReloadRef.current !== null ? seekToAfterReloadRef.current : -1,
      xhrSetup: (xhr) => {
        xhr.withCredentials = true;
      },
    });
    hlsRef.current = hls;
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      setHlsStatusMessage("");
      mediaRecoveryAttemptsRef.current = 0;
      networkRecoveryAttemptsRef.current = 0;
      markSubtitleReady();
    });
    hls.on(Hls.Events.ERROR, (_event, data: HlsErrorData) => {
      const formattedError = formatHlsErrorMessage(data);
      const isRecoverableGapError =
        !data.fatal &&
        (data.details === "bufferStalledError" || data.details === "bufferSeekOverHole");
      if (!isRecoverableGapError) {
        console.error("[PlaybackDock] HLS error", {
          mediaId: activeItemId,
          source: videoSourceUrl,
          fatal: data.fatal,
          type: data.type,
          details: data.details,
          error: data.error,
        });
      }

      if (!data.fatal) {
        if (data.details === "bufferStalledError") {
          const video = videoRef.current;
          if (maybeRecoverInitialBufferGap(video)) {
            setHlsStatusMessage("Resyncing playback...");
            void video?.play().catch(() => {});
          }
        }
        return;
      }

      if (data.type === Hls.ErrorTypes.NETWORK_ERROR && networkRecoveryAttemptsRef.current < 2) {
        networkRecoveryAttemptsRef.current += 1;
        setHlsStatusMessage("Reconnecting stream...");
        hls.startLoad();
        return;
      }

      if (data.type === Hls.ErrorTypes.MEDIA_ERROR && mediaRecoveryAttemptsRef.current < 2) {
        mediaRecoveryAttemptsRef.current += 1;
        setHlsStatusMessage("Recovering playback...");
        hls.recoverMediaError();
        return;
      }

      setHlsStatusMessage(`Stream error: ${formattedError}`);
    });
    hls.loadSource(videoSourceUrl);
    hls.attachMedia(video);

    return () => {
      hls.destroy();
      if (hlsRef.current === hls) {
        hlsRef.current = null;
      }
    };
  }, [
    activeItemId,
    isVideo,
    markSubtitleReady,
    maybeRecoverInitialBufferGap,
    videoAttachmentVersion,
    videoSourceUrl,
  ]);

  /* ── Close track menus on outside click ── */
  useEffect(() => {
    if (!subtitleMenuOpen && !audioMenuOpen && !playerSettingsOpen) return;
    const onClick = (e: MouseEvent) => {
      if (
        subtitleMenuRef.current?.contains(e.target as Node) ||
        subtitleBtnRef.current?.contains(e.target as Node) ||
        audioMenuRef.current?.contains(e.target as Node) ||
        audioBtnRef.current?.contains(e.target as Node) ||
        playerSettingsMenuRef.current?.contains(e.target as Node) ||
        playerSettingsBtnRef.current?.contains(e.target as Node)
      )
        return;
      setSubtitleMenuOpen(false);
      setAudioMenuOpen(false);
      setPlayerSettingsOpen(false);
    };
    document.addEventListener("pointerdown", onClick);
    return () => document.removeEventListener("pointerdown", onClick);
  }, [audioMenuOpen, playerSettingsOpen, subtitleMenuOpen]);

  const syncBrowserFullscreenState = useCallback(() => {
    setBrowserFullscreenActive(document.fullscreenElement === playerRootRef.current);
  }, []);

  useEffect(() => {
    syncBrowserFullscreenState();
    const handleFullscreenChange = () => syncBrowserFullscreenState();
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, [syncBrowserFullscreenState]);

  const toggleBrowserFullscreen = useCallback(async () => {
    if (document.fullscreenElement === playerRootRef.current) {
      await document.exitFullscreen().catch(() => {});
      return;
    }
    if (!playerRootRef.current) return;
    await playerRootRef.current.requestFullscreen?.().catch(() => {});
  }, []);

  useEffect(() => {
    if (!isFullscreen || !pendingBrowserFullscreen) return;
    void toggleBrowserFullscreen();
    setPendingBrowserFullscreen(false);
  }, [isFullscreen, pendingBrowserFullscreen, toggleBrowserFullscreen]);

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
          if (document.fullscreenElement === playerRootRef.current) {
            void document.exitFullscreen().catch(() => {});
          } else {
            primeVideoHandoff();
            exitFullscreen();
          }
          break;
        case "f":
        case "F":
          event.preventDefault();
          void toggleBrowserFullscreen();
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
    primeVideoHandoff,
    resetHideTimer,
    seekTo,
    setMuted,
    setVolume,
    toggleBrowserFullscreen,
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
  const showDefaultControls = isVideo && playerControlsAppearance === "default";
  const playButtonLabel = playbackState.isPlaying ? "Pause" : "Play";
  const muteButtonLabel = muted || volume === 0 ? "Unmute" : "Mute";
  const handleOpenFullscreen = () => {
    const snapshot = primeVideoHandoff();
    if (snapshot && (snapshot.positionSeconds > 0 || snapshot.ended)) {
      void persistPlaybackProgress({ force: true, snapshot });
    }
    enterFullscreen();
  };
  const handleReturnToDocked = () => {
    const snapshot = primeVideoHandoff();
    if (snapshot && (snapshot.positionSeconds > 0 || snapshot.ended)) {
      void persistPlaybackProgress({ force: true, snapshot });
    }
    exitFullscreen();
  };
  const handleClosePlayer = () => {
    const snapshot = captureVideoProgressSnapshot(videoRef.current);
    void persistPlaybackProgress({ force: true, snapshot });
    dismissDock();
  };

  /* ── Fullscreen video player ── */
  if (isFullscreen) {
    const seasonEpisode = getSeasonEpisodeLabel(activeItem);
    const titleDisplay = seasonEpisode
      ? `${seasonEpisode} · ${activeItem.title}`
      : activeItem.title;

    return (
      <section
        ref={(node) => {
          playerRootRef.current = node;
        }}
        className={`fullscreen-player fullscreen-player--controls-${playerControlsAppearance}${controlsVisible ? "" : " fullscreen-player--hidden"}`}
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
          style={videoSubtitleStyle}
          crossOrigin="use-credentials"
          autoPlay
          playsInline
          onLoadedMetadata={(event) => handleVideoLoadedMetadata(event.currentTarget)}
          onCanPlay={(event) => {
            maybeRecoverInitialBufferGap(event.currentTarget);
            syncPlaybackState(event.currentTarget);
            syncVideoProgressSnapshot(event.currentTarget);
            markSubtitleReady();
          }}
          onTimeUpdate={(event) => {
            if (event.currentTarget.currentTime > 1) {
              initialBufferGapHandledRef.current = true;
            }
            syncPlaybackState(event.currentTarget);
            syncVideoProgressSnapshot(event.currentTarget);
            persistInitialPlaybackProgress(event.currentTarget);
          }}
          onPlay={(event) => {
            if (event.currentTarget.currentTime > 1) {
              initialBufferGapHandledRef.current = true;
            }
            setHlsStatusMessage("");
            syncPlaybackState(event.currentTarget);
            syncVideoProgressSnapshot(event.currentTarget);
            persistInitialPlaybackProgress(event.currentTarget);
          }}
          onPause={(event) => {
            syncPlaybackState(event.currentTarget);
            const snapshot = captureVideoProgressSnapshot(event.currentTarget);
            void persistPlaybackProgress({ force: true, snapshot });
          }}
          onSeeked={(event) => {
            syncPlaybackState(event.currentTarget);
            syncVideoProgressSnapshot(event.currentTarget);
          }}
          onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
          onError={() => {
            setHlsStatusMessage("Stream error: browser media element failed to load playback");
          }}
          onEnded={(event) => {
            const snapshot = captureVideoProgressSnapshot(event.currentTarget);
            void persistPlaybackProgress({ force: true, completed: true, snapshot });
          }}
        ></video>

        {/* Top title bar */}
        <div className="fullscreen-player__top-bar">
          <div className="fullscreen-player__title-area">
            <h2 className="fullscreen-player__title">{titleDisplay}</h2>
            <div className="fullscreen-player__status">
              {videoStatusMessage && (
                <>
                  <span className="status-dot" data-connected={wsConnected} />
                  <span>{videoStatusMessage}</span>
                </>
              )}
            </div>
          </div>
          <button
            type="button"
            className="fullscreen-player__close-btn"
            onClick={handleReturnToDocked}
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
                className={`fullscreen-player__ctrl-btn${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                onClick={togglePlayPause}
                aria-label={playbackState.isPlaying ? "Pause playback" : "Play playback"}
              >
                {playbackState.isPlaying ? (
                  <Pause className="size-5" />
                ) : (
                  <Play className="size-5" />
                )}
                {showDefaultControls && <span>{playButtonLabel}</span>}
              </button>
              <span className="fullscreen-player__time">
                {formatClock(playbackState.currentTime)} / {formatClock(progressMax)}
              </span>
            </div>

            {/* Right: subtitles + settings + volume + fullscreen + exit */}
            <div className="fullscreen-player__controls-right">
              {subtitleTrackOptions.length > 0 && (
                <div className="fullscreen-player__subtitle-wrap">
                  <button
                    ref={subtitleBtnRef}
                    type="button"
                    className={`fullscreen-player__ctrl-btn${selectedSubtitleKey !== "off" ? " is-active" : ""}${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                    aria-label="Subtitles"
                    title="Subtitles"
                    onClick={() => {
                      setSubtitleMenuOpen((value) => !value);
                      setAudioMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                  >
                    <Subtitles className="size-5" />
                    {showDefaultControls && <span>Subtitles</span>}
                  </button>
                  {subtitleMenuOpen && (
                    <TrackMenu
                      menuRef={subtitleMenuRef}
                      options={subtitleTrackOptions}
                      selectedKey={selectedSubtitleKey}
                      ariaLabel="Select subtitle track"
                      offLabel="Off"
                      onSelect={(key) => {
                        setSelectedSubtitleKey(key);
                        setSubtitleMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}

              {audioTracks.length > 1 && (
                <div className="fullscreen-player__audio-wrap">
                  <button
                    ref={audioBtnRef}
                    type="button"
                    className={`fullscreen-player__ctrl-btn fullscreen-player__ctrl-btn--text${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                    aria-label={`Audio track: ${selectedAudioLabel}`}
                    title={`Audio track: ${selectedAudioLabel}`}
                    onClick={() => {
                      setAudioMenuOpen((value) => !value);
                      setSubtitleMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                  >
                    <Volume2 className="size-5" />
                    {showDefaultControls && <span>Audio</span>}
                  </button>
                  {audioMenuOpen && (
                    <TrackMenu
                      menuRef={audioMenuRef}
                      options={audioTracks}
                      selectedKey={selectedAudioKey}
                      ariaLabel="Select audio track"
                      onSelect={(key) => {
                        requestedAudioTrackRef.current =
                          activeItem != null ? { mediaId: activeItem.id, key } : null;
                        setSelectedAudioKey(key);
                        setAudioMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}

              {isVideo && (
                <div className="fullscreen-player__settings-wrap">
                  <button
                    ref={playerSettingsBtnRef}
                    type="button"
                    className={`fullscreen-player__ctrl-btn${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                    aria-label="Subtitle settings"
                    title="Subtitle settings"
                    onClick={() => {
                      setPlayerSettingsOpen((value) => !value);
                      setSubtitleMenuOpen(false);
                      setAudioMenuOpen(false);
                    }}
                  >
                    <Settings className="size-5" />
                    {showDefaultControls && <span>Player</span>}
                  </button>
                  {playerSettingsOpen && (
                    <PlayerSettingsMenu
                      menuRef={playerSettingsMenuRef}
                      preferences={subtitleAppearance}
                      controlsAppearance={playerControlsAppearance}
                      onChange={setSubtitleAppearance}
                      onControlsAppearanceChange={setPlayerControlsAppearance}
                    />
                  )}
                </div>
              )}

              <div className="fullscreen-player__volume-group">
                <button
                  type="button"
                  className={`fullscreen-player__ctrl-btn${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                  onClick={() => setMuted(!muted)}
                  aria-label={muteButtonLabel}
                >
                  {muted || volume === 0 ? (
                    <VolumeX className="size-5" />
                  ) : (
                    <Volume2 className="size-5" />
                  )}
                  {showDefaultControls && <span>{muteButtonLabel}</span>}
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
                className={`fullscreen-player__ctrl-btn${browserFullscreenActive ? " is-active" : ""}${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                onClick={() => {
                  void toggleBrowserFullscreen();
                }}
                aria-label={
                  browserFullscreenActive ? "Exit true fullscreen" : "Enter true fullscreen"
                }
                title={browserFullscreenActive ? "Exit true fullscreen" : "Enter true fullscreen"}
              >
                <span className="player-fullscreen-icon" aria-hidden="true" />
                {showDefaultControls && (
                  <span>{browserFullscreenActive ? "Window" : "Fullscreen"}</span>
                )}
              </button>

              <button
                type="button"
                className={`fullscreen-player__ctrl-btn${showDefaultControls ? " fullscreen-player__ctrl-btn--labeled" : ""}`}
                onClick={handleReturnToDocked}
                aria-label="Return to docked player"
                title="Return to docked player"
              >
                <Minimize2 className="size-4" />
                {showDefaultControls && <span>Docked</span>}
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
      ref={(node) => {
        playerRootRef.current = node;
      }}
      className={`playback-dock playback-dock--${activeMode} playback-dock--${viewMode} playback-dock--controls-${playerControlsAppearance}`}
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
                <span className="playback-dock__status-copy">{videoStatusMessage}</span>
              </>
            )}
          </div>
          <div className="playback-dock__actions">
            {isVideo && (
              <button
                type="button"
                className="playback-dock__icon-button"
                onClick={handleOpenFullscreen}
                aria-label="Open fullscreen player"
                title="Open fullscreen player"
              >
                <Expand className="size-4" />
              </button>
            )}
            <button
              type="button"
              className="playback-dock__icon-button"
              onClick={handleClosePlayer}
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
                <img src="/placeholder-poster.svg" alt="" />
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
              {isVideo && subtitleTrackOptions.length > 0 && (
                <div className="playback-dock__subtitle-picker">
                  <button
                    ref={subtitleBtnRef}
                    type="button"
                    className={`playback-dock__subtitle-btn${selectedSubtitleKey !== "off" ? " is-active" : ""}`}
                    onClick={() => {
                      setSubtitleMenuOpen((value) => !value);
                      setAudioMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                    aria-label="Subtitles"
                  >
                    <Subtitles className="size-4" />
                    {showDefaultControls && <span>Subtitles</span>}
                  </button>
                  {subtitleMenuOpen && (
                    <TrackMenu
                      menuRef={subtitleMenuRef}
                      options={subtitleTrackOptions}
                      selectedKey={selectedSubtitleKey}
                      position="above"
                      ariaLabel="Select subtitle track"
                      offLabel="Off"
                      onSelect={(key) => {
                        setSelectedSubtitleKey(key);
                        setSubtitleMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}
              {isVideo && audioTracks.length > 1 && (
                <div className="playback-dock__audio-picker">
                  <button
                    ref={audioBtnRef}
                    type="button"
                    className="playback-dock__audio-btn"
                    onClick={() => {
                      setAudioMenuOpen((value) => !value);
                      setSubtitleMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                    aria-label={`Audio track: ${selectedAudioLabel}`}
                  >
                    <Volume2 className="size-4" />
                    {showDefaultControls ? <span>{selectedAudioLabel}</span> : null}
                  </button>
                  {audioMenuOpen && (
                    <TrackMenu
                      menuRef={audioMenuRef}
                      options={audioTracks}
                      selectedKey={selectedAudioKey}
                      position="above"
                      ariaLabel="Select audio track"
                      onSelect={(key) => {
                        requestedAudioTrackRef.current =
                          activeItem != null ? { mediaId: activeItem.id, key } : null;
                        setSelectedAudioKey(key);
                        setAudioMenuOpen(false);
                      }}
                    />
                  )}
                </div>
              )}
              {isVideo && (
                <div className="playback-dock__subtitle-picker">
                  <button
                    ref={playerSettingsBtnRef}
                    type="button"
                    className="playback-dock__subtitle-btn"
                    onClick={() => {
                      setPlayerSettingsOpen((value) => !value);
                      setSubtitleMenuOpen(false);
                      setAudioMenuOpen(false);
                    }}
                    aria-label="Subtitle settings"
                  >
                    <Settings className="size-4" />
                    {showDefaultControls && <span>Player</span>}
                  </button>
                  {playerSettingsOpen && (
                    <PlayerSettingsMenu
                      menuRef={playerSettingsMenuRef}
                      preferences={subtitleAppearance}
                      controlsAppearance={playerControlsAppearance}
                      onChange={setSubtitleAppearance}
                      onControlsAppearanceChange={setPlayerControlsAppearance}
                    />
                  )}
                </div>
              )}
            </div>
          </div>

          {isVideo && (
            <div
              className="playback-dock__surface"
              onClick={handleOpenFullscreen}
              aria-label={`Open fullscreen player for ${activeItem.title}`}
              role="button"
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  handleOpenFullscreen();
                }
              }}
            >
              <video
                key={activeItem.id}
                ref={setVideoRef}
                className="playback-dock__video"
                style={videoSubtitleStyle}
                crossOrigin="use-credentials"
                autoPlay
                playsInline
                onLoadedMetadata={(event) => handleVideoLoadedMetadata(event.currentTarget)}
                onCanPlay={(event) => {
                  maybeRecoverInitialBufferGap(event.currentTarget);
                  syncPlaybackState(event.currentTarget);
                  syncVideoProgressSnapshot(event.currentTarget);
                  markSubtitleReady();
                }}
                onTimeUpdate={(event) => {
                  if (event.currentTarget.currentTime > 1) {
                    initialBufferGapHandledRef.current = true;
                  }
                  syncPlaybackState(event.currentTarget);
                  syncVideoProgressSnapshot(event.currentTarget);
                  persistInitialPlaybackProgress(event.currentTarget);
                }}
                onPlay={(event) => {
                  if (event.currentTarget.currentTime > 1) {
                    initialBufferGapHandledRef.current = true;
                  }
                  setHlsStatusMessage("");
                  syncPlaybackState(event.currentTarget);
                  syncVideoProgressSnapshot(event.currentTarget);
                  persistInitialPlaybackProgress(event.currentTarget);
                }}
                onPause={(event) => {
                  syncPlaybackState(event.currentTarget);
                  const snapshot = captureVideoProgressSnapshot(event.currentTarget);
                  void persistPlaybackProgress({ force: true, snapshot });
                }}
                onSeeked={(event) => {
                  syncPlaybackState(event.currentTarget);
                  syncVideoProgressSnapshot(event.currentTarget);
                }}
                onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
                onError={() => {
                  setHlsStatusMessage(
                    "Stream error: browser media element failed to load playback",
                  );
                }}
                onEnded={(event) => {
                  const snapshot = captureVideoProgressSnapshot(event.currentTarget);
                  void persistPlaybackProgress({ force: true, completed: true, snapshot });
                }}
              ></video>
              <button
                type="button"
                className={`playback-dock__true-fullscreen-btn${browserFullscreenActive ? " is-active" : ""}`}
                aria-label="Enter true fullscreen"
                title="Enter true fullscreen"
                onClick={(event) => {
                  event.stopPropagation();
                  handleOpenFullscreen();
                  setPendingBrowserFullscreen(true);
                }}
              >
                <span className="player-fullscreen-icon" aria-hidden="true" />
              </button>
              <span className="playback-dock__surface-hint">Click video to expand</span>
            </div>
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
              className={`playback-dock__play-button${showDefaultControls ? " playback-dock__play-button--labeled" : ""}`}
              onClick={togglePlayPause}
              aria-label={playbackState.isPlaying ? "Pause playback" : "Play playback"}
            >
              {playbackState.isPlaying ? <Pause className="size-5" /> : <Play className="size-5" />}
              {showDefaultControls && <span>{playButtonLabel}</span>}
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
              className={`playback-dock__icon-button${showDefaultControls ? " playback-dock__icon-button--labeled" : ""}`}
              onClick={() => setMuted(!muted)}
              aria-label={muteButtonLabel}
            >
              {muted || volume === 0 ? (
                <VolumeX className="size-4" />
              ) : (
                <Volume2 className="size-4" />
              )}
              {showDefaultControls && <span>{muteButtonLabel}</span>}
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
