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
import type { MediaItem } from "../api";
import { BASE_URL, updateMediaProgress } from "../api";
import { usePlayer } from "../contexts/PlayerContext";
import {
  languageMatchesPreference,
  readStoredSubtitleAppearance,
  resolveLibraryPlaybackPreferences,
  subtitleFontSizeValue,
  subtitlePositionOptions,
  subtitleSizeOptions,
  writeStoredSubtitleAppearance,
  type SubtitleAppearance,
} from "../lib/playbackPreferences";
import { queryKeys, useLibraries } from "../queries";

type PlaybackState = {
  currentTime: number;
  duration: number;
  isPlaying: boolean;
};

type TrackMenuOption = {
  key: string;
  label: string;
};

type SubtitleTrackOption = TrackMenuOption & {
  src: string;
  srcLang: string;
};

type AudioTrackOption = TrackMenuOption & {
  streamIndex: number;
  language: string;
};

type BrowserAudioTrack = {
  enabled: boolean;
};

type BrowserAudioTrackList = {
  length: number;
  [index: number]: BrowserAudioTrack | undefined;
};

function getBrowserAudioTracks(element: HTMLVideoElement | null): BrowserAudioTrackList | null {
  if (!element) return null;
  const audioTracks = (element as HTMLVideoElement & { audioTracks?: BrowserAudioTrackList })
    .audioTracks;
  return audioTracks && typeof audioTracks.length === "number" ? audioTracks : null;
}

function formatTrackLabel(
  title: string | undefined,
  language: string | undefined,
  fallback: string,
): string {
  const normalizedTitle = title?.trim();
  const normalizedLanguage = language?.trim();
  if (
    normalizedTitle &&
    normalizedLanguage &&
    normalizedTitle.localeCompare(normalizedLanguage, undefined, { sensitivity: "accent" }) !== 0
  ) {
    return `${normalizedTitle} • ${normalizedLanguage}`;
  }
  return normalizedTitle || normalizedLanguage || fallback;
}

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

function getPreferredSubtitleKey(
  subtitleTracks: SubtitleTrackOption[],
  preferredLanguage: string,
  subtitlesEnabled: boolean,
): string {
  if (!subtitlesEnabled || !preferredLanguage) return "off";
  return (
    subtitleTracks.find(
      (track) =>
        languageMatchesPreference(track.srcLang, preferredLanguage) ||
        languageMatchesPreference(track.label, preferredLanguage),
    )?.key ?? "off"
  );
}

function getPreferredAudioKey(audioTracks: AudioTrackOption[], preferredLanguage: string): string {
  if (!preferredLanguage) return "";
  return (
    audioTracks.find(
      (track) =>
        languageMatchesPreference(track.language, preferredLanguage) ||
        languageMatchesPreference(track.label, preferredLanguage),
    )?.key ?? ""
  );
}

function applyCueLineSetting(cue: TextTrackCue, position: SubtitleAppearance["position"]) {
  const cueWithLine = cue as TextTrackCue & { line?: number | string };
  if (!("line" in cueWithLine)) return;
  cueWithLine.line = position === "top" ? 8 : "auto";
}

function applySubtitleCuePosition(
  element: HTMLVideoElement | null,
  position: SubtitleAppearance["position"],
) {
  if (!element) return;
  for (let trackIndex = 0; trackIndex < element.textTracks.length; trackIndex += 1) {
    const track = element.textTracks[trackIndex];
    const cues = track?.cues;
    if (!track || !cues) continue;
    for (let cueIndex = 0; cueIndex < cues.length; cueIndex += 1) {
      const cue = cues[cueIndex];
      if (!cue) continue;
      applyCueLineSetting(cue, position);
    }
  }
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
  onChange,
}: {
  menuRef: RefObject<HTMLDivElement | null>;
  preferences: SubtitleAppearance;
  onChange: (value: SubtitleAppearance) => void;
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
  const [selectedSubtitleKey, setSelectedSubtitleKey] = useState("off");
  const [selectedAudioKey, setSelectedAudioKey] = useState("");
  const [subtitleTrackVersion, setSubtitleTrackVersion] = useState(0);
  const [audioTrackVersion, setAudioTrackVersion] = useState(0);
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
  const [controlsVisible, setControlsVisible] = useState(true);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout>>(0);
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const seekToAfterReloadRef = useRef<number | null>(null);
  const resumePlaybackAfterReloadRef = useRef(false);
  const handledTranscodeVersionRef = useRef(0);
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
    transcodeVersion,
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

  const isVideo = activeMode === "video" && activeItem != null;
  const isMusic = activeMode === "music" && activeItem != null;
  const isFullscreen = isVideo && viewMode === "fullscreen";
  const activeItemId = activeItem?.id ?? null;
  const activeItemDuration = activeItem?.duration ?? 0;
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
    if (transcodeVersion <= 0 || handledTranscodeVersionRef.current === transcodeVersion) return;
    const video = videoRef.current;
    if (!video) return;
    handledTranscodeVersionRef.current = transcodeVersion;
    seekToAfterReloadRef.current =
      Number.isFinite(video.currentTime) && video.currentTime > 0
        ? video.currentTime
        : playbackState.currentTime;
    resumePlaybackAfterReloadRef.current = !video.paused && !video.ended;
    video.pause();
    video.load();
  }, [playbackState.currentTime, transcodeVersion]);

  const subtitleTracks = useMemo<SubtitleTrackOption[]>(() => {
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

  const persistPlaybackProgress = useCallback(
    async (options?: { force?: boolean; completed?: boolean }) => {
      if (!isVideo || !activeItem) return;
      const video = videoRef.current;
      if (!video) return;
      const duration =
        Number.isFinite(video.duration) && video.duration > 0
          ? video.duration
          : activeItem.duration;
      if (!Number.isFinite(duration) || duration <= 0) return;
      const positionSeconds = Math.max(
        0,
        Math.min(
          Number.isFinite(video.currentTime) ? video.currentTime : playbackState.currentTime,
          duration,
        ),
      );
      const completed = options?.completed === true;
      const previous = lastPersistedRef.current;
      if (
        !options?.force &&
        previous?.mediaId === activeItem.id &&
        previous.completed === completed &&
        Math.abs(previous.positionSeconds - positionSeconds) < 10
      ) {
        return;
      }
      await updateMediaProgress(activeItem.id, {
        position_seconds: positionSeconds,
        duration_seconds: duration,
        completed,
      }).catch(() => {});
      lastPersistedRef.current = {
        mediaId: activeItem.id,
        positionSeconds,
        completed,
      };
      if (activeItem.library_id != null) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.library(activeItem.library_id) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.home });
    },
    [activeItem, isVideo, playbackState.currentTime, queryClient],
  );

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
      const maxResumeTime =
        Number.isFinite(element.duration) && element.duration > 0 ? element.duration - 1 : resumeAt;
      element.currentTime = Math.max(0, Math.min(resumeAt, maxResumeTime));
      resumeAppliedRef.current = activeItem.id;
    },
    [activeItem, isVideo],
  );

  const setVideoRef = useCallback(
    (element: HTMLVideoElement | null) => {
      if (element == null && hlsRef.current != null) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
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
    },
    [applyResumePosition, syncPlaybackState],
  );

  useEffect(() => {
    setPlaybackState({
      currentTime: 0,
      duration: activeItemDuration,
      isPlaying: false,
    });
    setSelectedSubtitleKey("off");
    setSelectedAudioKey("");
    setSubtitleTrackVersion(0);
    setAudioTrackVersion(0);
    resumeAppliedRef.current = null;
    defaultTrackSelectionAppliedRef.current = null;
    requestedAudioTrackRef.current =
      activeItemId != null ? { mediaId: activeItemId, key: "" } : null;
    seekToAfterReloadRef.current = null;
    resumePlaybackAfterReloadRef.current = false;
    handledTranscodeVersionRef.current = 0;
    setSubtitleMenuOpen(false);
    setAudioMenuOpen(false);
    setPlayerSettingsOpen(false);
  }, [activeItemDuration, activeItemId]);

  useEffect(() => {
    if (!activeItem) {
      requestedAudioTrackRef.current = null;
      return;
    }
    if (requestedAudioTrackRef.current?.mediaId !== activeItem.id) {
      requestedAudioTrackRef.current = { mediaId: activeItem.id, key: audioTracks[0]?.key ?? "" };
      return;
    }
    if (!requestedAudioTrackRef.current.key && audioTracks[0]?.key) {
      requestedAudioTrackRef.current = { mediaId: activeItem.id, key: audioTracks[0].key };
    }
  }, [activeItem, audioTracks]);

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
    if (!isVideo || !activeItem) return;
    if (defaultTrackSelectionAppliedRef.current === activeItem.id) return;
    if (activeItem.library_id != null && !librariesFetched) return;
    setSelectedSubtitleKey(
      getPreferredSubtitleKey(
        subtitleTracks,
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
    subtitleTracks,
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

  /* ── Subtitle track activation ── */
  const activateSubtitleTrack = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    for (let i = 0; i < video.textTracks.length; i += 1) {
      const tt = video.textTracks[i];
      if (!tt) continue;
      tt.mode = i === selectedSubtitleIndex ? "showing" : "disabled";
    }
    applySubtitleCuePosition(video, subtitleAppearance.position);
  }, [selectedSubtitleIndex, subtitleAppearance.position]);

  useEffect(() => {
    activateSubtitleTrack();
  }, [activateSubtitleTrack, subtitleTrackVersion]);

  useEffect(() => {
    applySubtitleCuePosition(videoRef.current, subtitleAppearance.position);
  }, [subtitleAppearance.position, subtitleTrackVersion]);

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
      const previousRequest = requestedAudioTrackRef.current;
      if (previousRequest?.mediaId === activeItem.id && previousRequest.key === key) return;
      requestedAudioTrackRef.current = { mediaId: activeItem.id, key };
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
    requestAudioTrackChange(selectedAudioKey);
  }, [requestAudioTrackChange, selectedAudioKey, syncBrowserAudioTrackSelection, videoSourceUrl]);

  useEffect(() => {
    const video = videoRef.current;
    if (!isVideo || !video) return;

    if (hlsRef.current != null) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }

    if (!videoSourceUrl) {
      video.removeAttribute("src");
      video.load();
      return;
    }

    if (video.canPlayType("application/vnd.apple.mpegurl")) {
      if (video.src !== videoSourceUrl) {
        video.src = videoSourceUrl;
      }
      return;
    }

    if (!Hls.isSupported()) {
      video.src = videoSourceUrl;
      return;
    }

    const hls = new Hls({
      enableWorker: true,
      backBufferLength: 90,
    });
    hlsRef.current = hls;
    hls.loadSource(videoSourceUrl);
    hls.attachMedia(video);

    return () => {
      hls.destroy();
      if (hlsRef.current === hls) {
        hlsRef.current = null;
      }
    };
  }, [isVideo, videoSourceUrl]);

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
          style={videoSubtitleStyle}
          crossOrigin="use-credentials"
          autoPlay
          playsInline
          onLoadedMetadata={(event) => handleVideoLoadedMetadata(event.currentTarget)}
          onTimeUpdate={(event) => syncPlaybackState(event.currentTarget)}
          onPlay={(event) => syncPlaybackState(event.currentTarget)}
          onPause={(event) => {
            syncPlaybackState(event.currentTarget);
            void persistPlaybackProgress({ force: true });
          }}
          onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
          onEnded={() => {
            void persistPlaybackProgress({ force: true, completed: true });
          }}
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
            onClick={() => {
              void persistPlaybackProgress({ force: true });
              exitFullscreen();
            }}
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

            {/* Right: subtitles + settings + volume + fullscreen + exit */}
            <div className="fullscreen-player__controls-right">
              {subtitleTracks.length > 0 && (
                <div className="fullscreen-player__subtitle-wrap">
                  <button
                    ref={subtitleBtnRef}
                    type="button"
                    className={`fullscreen-player__ctrl-btn${selectedSubtitleKey !== "off" ? " is-active" : ""}`}
                    aria-label="Subtitles"
                    title="Subtitles"
                    onClick={() => {
                      setSubtitleMenuOpen((value) => !value);
                      setAudioMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                  >
                    <Subtitles className="size-5" />
                  </button>
                  {subtitleMenuOpen && (
                    <TrackMenu
                      menuRef={subtitleMenuRef}
                      options={subtitleTracks}
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
                    className="fullscreen-player__ctrl-btn fullscreen-player__ctrl-btn--text"
                    aria-label={`Audio track: ${selectedAudioLabel}`}
                    title={`Audio track: ${selectedAudioLabel}`}
                    onClick={() => {
                      setAudioMenuOpen((value) => !value);
                      setSubtitleMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                  >
                    <span>Audio</span>
                  </button>
                  {audioMenuOpen && (
                    <TrackMenu
                      menuRef={audioMenuRef}
                      options={audioTracks}
                      selectedKey={selectedAudioKey}
                      ariaLabel="Select audio track"
                      onSelect={(key) => {
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
                    className="fullscreen-player__ctrl-btn"
                    aria-label="Subtitle settings"
                    title="Subtitle settings"
                    onClick={() => {
                      setPlayerSettingsOpen((value) => !value);
                      setSubtitleMenuOpen(false);
                      setAudioMenuOpen(false);
                    }}
                  >
                    <Settings className="size-5" />
                  </button>
                  {playerSettingsOpen && (
                    <PlayerSettingsMenu
                      menuRef={playerSettingsMenuRef}
                      preferences={subtitleAppearance}
                      onChange={setSubtitleAppearance}
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
                className={`fullscreen-player__ctrl-btn${browserFullscreenActive ? " is-active" : ""}`}
                onClick={() => {
                  void toggleBrowserFullscreen();
                }}
                aria-label={
                  browserFullscreenActive ? "Exit true fullscreen" : "Enter true fullscreen"
                }
                title={browserFullscreenActive ? "Exit true fullscreen" : "Enter true fullscreen"}
              >
                <span className="player-fullscreen-icon" aria-hidden="true" />
              </button>

              <button
                type="button"
                className="fullscreen-player__ctrl-btn"
                onClick={() => {
                  void persistPlaybackProgress({ force: true });
                  exitFullscreen();
                }}
                aria-label="Return to docked player"
                title="Return to docked player"
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
      ref={(node) => {
        playerRootRef.current = node;
      }}
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
              onClick={() => {
                void persistPlaybackProgress({ force: true });
                dismissDock();
              }}
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
                    onClick={() => {
                      setSubtitleMenuOpen((value) => !value);
                      setAudioMenuOpen(false);
                      setPlayerSettingsOpen(false);
                    }}
                    aria-label="Subtitles"
                  >
                    <Subtitles className="size-4" />
                    <span>Subtitles</span>
                  </button>
                  {subtitleMenuOpen && (
                    <TrackMenu
                      menuRef={subtitleMenuRef}
                      options={subtitleTracks}
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
                    <span>Audio</span>
                    <span>{selectedAudioLabel}</span>
                  </button>
                  {audioMenuOpen && (
                    <TrackMenu
                      menuRef={audioMenuRef}
                      options={audioTracks}
                      selectedKey={selectedAudioKey}
                      position="above"
                      ariaLabel="Select audio track"
                      onSelect={(key) => {
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
                    <span>Subtitle style</span>
                  </button>
                  {playerSettingsOpen && (
                    <PlayerSettingsMenu
                      menuRef={playerSettingsMenuRef}
                      preferences={subtitleAppearance}
                      onChange={setSubtitleAppearance}
                    />
                  )}
                </div>
              )}
            </div>
          </div>

          {isVideo && (
            <div
              className="playback-dock__surface"
              onClick={enterFullscreen}
              aria-label={`Open fullscreen player for ${activeItem.title}`}
              role="button"
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  enterFullscreen();
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
                onTimeUpdate={(event) => syncPlaybackState(event.currentTarget)}
                onPlay={(event) => syncPlaybackState(event.currentTarget)}
                onPause={(event) => {
                  syncPlaybackState(event.currentTarget);
                  void persistPlaybackProgress({ force: true });
                }}
                onVolumeChange={(event) => syncPlaybackState(event.currentTarget)}
                onEnded={() => {
                  void persistPlaybackProgress({ force: true, completed: true });
                }}
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
              <button
                type="button"
                className={`playback-dock__true-fullscreen-btn${browserFullscreenActive ? " is-active" : ""}`}
                aria-label="Enter true fullscreen"
                title="Enter true fullscreen"
                onClick={(event) => {
                  event.stopPropagation();
                  enterFullscreen();
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
