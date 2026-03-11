import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  buildPlumWebSocketUrl,
  parsePlumWebSocketEvent,
  playbackSessionPlaylistUrl,
} from "@plum/shared";
import type { MediaItem, PlaybackSession as ApiPlaybackSession } from "../api";
import {
  BASE_URL,
  closePlaybackSession,
  createPlaybackSession,
  updatePlaybackSessionAudio,
} from "../api";
import { sortMusicTracks } from "../lib/musicGrouping";
import { sortEpisodes } from "../lib/showGrouping";

export type PlaybackKind = "video" | "music";
export type PlayerViewMode = "docked" | "fullscreen";
export type MusicRepeatMode = "off" | "all" | "one";
export type MediaElementSlot = "audio" | "video";

export type PlaybackSession = {
  activeMode: PlaybackKind;
  isDockOpen: boolean;
  viewMode: PlayerViewMode;
  queue: MediaItem[];
  queueIndex: number;
  shuffle: boolean;
  repeatMode: MusicRepeatMode;
};

type VideoSessionState = {
  sessionId: string;
  mediaId: number;
  desiredRevision: number;
  currentRevision: number;
  audioIndex: number;
  status: "starting" | "ready" | "error" | "closed";
  playlistPath: string;
  error: string;
};

type PlayerContextValue = {
  playbackSession: PlaybackSession | null;
  activeItem: MediaItem | null;
  activeMode: PlaybackKind | null;
  isDockOpen: boolean;
  viewMode: PlayerViewMode;
  queue: MediaItem[];
  queueIndex: number;
  shuffle: boolean;
  repeatMode: MusicRepeatMode;
  volume: number;
  muted: boolean;
  transcodeVersion: number;
  videoSourceUrl: string;
  playMedia: (item: MediaItem) => void;
  playMovie: (item: MediaItem) => void;
  playEpisode: (item: MediaItem) => void;
  playShowGroup: (items: MediaItem[], startItem?: MediaItem) => void;
  playMusicCollection: (items: MediaItem[], startItem?: MediaItem) => void;
  dismissDock: () => void;
  closePlayer: () => void;
  togglePlayPause: () => void;
  seekTo: (seconds: number) => void;
  setMuted: (muted: boolean) => void;
  setVolume: (volume: number) => void;
  enterFullscreen: () => void;
  exitFullscreen: () => void;
  registerMediaElement: (slot: MediaElementSlot, element: HTMLMediaElement | null) => void;
  playNextInQueue: () => void;
  playPreviousInQueue: () => void;
  toggleShuffle: () => void;
  cycleRepeatMode: () => void;
  changeAudioTrack: (audioIndex: number) => Promise<void>;
  wsConnected: boolean;
  lastEvent: string;
};

const PlayerContext = createContext<PlayerContextValue | null>(null);

function shuffleQueue(items: MediaItem[], currentId: number): MediaItem[] {
  const current = items.find((item) => item.id === currentId) ?? items[0];
  const rest = items.filter((item) => item.id !== current?.id);
  for (let index = rest.length - 1; index > 0; index -= 1) {
    const swapIndex = Math.floor(Math.random() * (index + 1));
    [rest[index], rest[swapIndex]] = [rest[swapIndex], rest[index]];
  }
  return current ? [current, ...rest] : rest;
}

function indexOfQueueItem(items: MediaItem[], itemId: number): number {
  return items.findIndex((item) => item.id === itemId);
}

function clampVolume(volume: number): number {
  return Math.max(0, Math.min(volume, 1));
}

export function PlayerProvider({ children }: { children: ReactNode }) {
  const [playbackSession, setPlaybackSession] = useState<PlaybackSession | null>(null);
  const [videoSession, setVideoSession] = useState<VideoSessionState | null>(null);
  const [musicBaseQueue, setMusicBaseQueue] = useState<MediaItem[]>([]);
  const [volume, setVolumeState] = useState(1);
  const [muted, setMutedState] = useState(false);
  const [transcodeVersion, setTranscodeVersion] = useState(0);
  const [wsConnected, setWsConnected] = useState(false);
  const [lastEvent, setLastEvent] = useState("");
  const wsRef = useRef<WebSocket | null>(null);
  const connectTimeoutRef = useRef<ReturnType<typeof setTimeout>>(0);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout>>(0);
  const mountedRef = useRef(true);
  const activeVideoItemIdRef = useRef<number | null>(null);
  const videoSessionRef = useRef<VideoSessionState | null>(null);
  const mediaElementsRef = useRef<Record<MediaElementSlot, HTMLMediaElement | null>>({
    audio: null,
    video: null,
  });

  const activeItem = playbackSession?.queue[playbackSession.queueIndex] ?? null;
  const activeMode = playbackSession?.activeMode ?? null;
  const isDockOpen = playbackSession?.isDockOpen ?? false;
  const viewMode = playbackSession?.viewMode ?? "docked";
  const queue = useMemo(() => playbackSession?.queue ?? [], [playbackSession]);
  const queueIndex = playbackSession?.queueIndex ?? 0;
  const shuffle = playbackSession?.shuffle ?? false;
  const repeatMode = playbackSession?.repeatMode ?? "off";

  activeVideoItemIdRef.current = activeMode === "video" ? (activeItem?.id ?? null) : null;
  videoSessionRef.current = videoSession;

  const videoSourceUrl =
    activeMode === "video" && videoSession != null && videoSession.currentRevision > 0
      ? playbackSessionPlaylistUrl(BASE_URL, videoSession.sessionId, videoSession.currentRevision)
      : "";

  const closeVideoSession = useCallback((sessionId?: string | null) => {
    if (!sessionId) return;
    closePlaybackSession(sessionId).catch(() => {});
  }, []);

  const applyPlaybackSession = useCallback((session: ApiPlaybackSession) => {
    setVideoSession({
      sessionId: session.sessionId,
      mediaId: session.mediaId,
      desiredRevision: session.revision,
      currentRevision: session.status === "ready" ? session.revision : 0,
      audioIndex: session.audioIndex,
      status: session.status,
      playlistPath: session.playlistPath,
      error: session.error ?? "",
    });
    if (session.status === "ready") {
      setTranscodeVersion(session.revision);
      setLastEvent("Stream ready");
    } else {
      setTranscodeVersion(0);
      setLastEvent("Preparing stream...");
    }
  }, []);

  const pauseAllMediaElements = useCallback(() => {
    mediaElementsRef.current.audio?.pause();
    mediaElementsRef.current.video?.pause();
  }, []);

  const exitBrowserFullscreen = useCallback(() => {
    if (!document.fullscreenElement) return;
    void document.exitFullscreen().catch(() => {});
  }, []);

  const registerMediaElement = useCallback(
    (slot: MediaElementSlot, element: HTMLMediaElement | null) => {
      mediaElementsRef.current[slot] = element;
      if (!element) return;
      element.volume = volume;
      element.muted = muted;
    },
    [muted, volume],
  );

  useEffect(() => {
    for (const element of Object.values(mediaElementsRef.current)) {
      if (!element) continue;
      element.volume = volume;
      element.muted = muted;
    }
  }, [muted, volume]);

  const getActiveMediaElement = useCallback(() => {
    if (activeMode === "music") return mediaElementsRef.current.audio;
    if (activeMode === "video") return mediaElementsRef.current.video;
    return null;
  }, [activeMode]);

  const dismissDock = useCallback(() => {
    if (playbackSession?.activeMode === "video") {
      closeVideoSession(videoSessionRef.current?.sessionId);
    }
    pauseAllMediaElements();
    exitBrowserFullscreen();
    setPlaybackSession(null);
    setVideoSession(null);
    setMusicBaseQueue([]);
    setTranscodeVersion(0);
  }, [
    closeVideoSession,
    exitBrowserFullscreen,
    pauseAllMediaElements,
    playbackSession?.activeMode,
  ]);

  const playVideoQueue = useCallback(
    (items: MediaItem[], startIndex = 0) => {
      if (items.length === 0) return;
      pauseAllMediaElements();
      const clampedIndex = Math.max(0, Math.min(startIndex, items.length - 1));
      const nextItem = items[clampedIndex] ?? items[0];
      setPlaybackSession((current) => ({
        activeMode: "video",
        isDockOpen: true,
        viewMode: "fullscreen",
        queue: items,
        queueIndex: clampedIndex,
        shuffle: false,
        repeatMode: current?.repeatMode ?? "off",
      }));
      closeVideoSession(videoSessionRef.current?.sessionId);
      setVideoSession(null);
      setMusicBaseQueue([]);
      setTranscodeVersion(0);
      if (!nextItem) return;
      createPlaybackSession(nextItem.id, { audioIndex: -1 })
        .then((session) => {
          if (!mountedRef.current) return;
          applyPlaybackSession(session);
        })
        .catch((err) => {
          console.error("[Player] createPlaybackSession failed", err);
          setLastEvent(`Error: ${err instanceof Error ? err.message : "Failed to start playback"}`);
        });
    },
    [applyPlaybackSession, closeVideoSession, pauseAllMediaElements],
  );

  const changeAudioTrack = useCallback(
    async (audioIndex: number) => {
      const session = videoSessionRef.current;
      if (activeMode !== "video" || !activeItem || !session) return;
      setLastEvent("Switching audio track...");
      try {
        const nextSession = await updatePlaybackSessionAudio(session.sessionId, { audioIndex });
        setVideoSession((current) =>
          current == null || current.sessionId !== nextSession.sessionId
            ? current
            : {
                ...current,
                desiredRevision: nextSession.revision,
                audioIndex: nextSession.audioIndex,
                status: nextSession.status,
                playlistPath: nextSession.playlistPath,
                error: nextSession.error ?? "",
              },
        );
      } catch (err) {
        console.error("[Player] changeAudioTrack failed", err);
        setLastEvent(
          `Error: ${err instanceof Error ? err.message : "Failed to switch audio track"}`,
        );
      }
    },
    [activeItem, activeMode],
  );

  const playMovie = useCallback(
    (item: MediaItem) => {
      playVideoQueue([item]);
    },
    [playVideoQueue],
  );

  const playEpisode = useCallback(
    (item: MediaItem) => {
      playVideoQueue([item]);
    },
    [playVideoQueue],
  );

  const playShowGroup = useCallback(
    (items: MediaItem[], startItem?: MediaItem) => {
      if (items.length === 0) return;
      const episodes = [...items];
      sortEpisodes(episodes);
      const startIndex =
        startItem == null
          ? 0
          : Math.max(
              0,
              episodes.findIndex((episode) => episode.id === startItem.id),
            );
      playVideoQueue(episodes, startIndex);
    },
    [playVideoQueue],
  );

  const playMusicCollection = useCallback(
    (items: MediaItem[], startItem?: MediaItem) => {
      const baseQueue = sortMusicTracks(items.filter((item) => item.type === "music"));
      if (baseQueue.length === 0) return;

      pauseAllMediaElements();

      const target = startItem ?? baseQueue[0];
      const nextShuffle = activeMode === "music" ? shuffle : false;
      const nextRepeatMode = activeMode === "music" ? repeatMode : "off";
      const orderedQueue = nextShuffle ? shuffleQueue(baseQueue, target.id) : baseQueue;
      const nextIndex = Math.max(0, indexOfQueueItem(orderedQueue, target.id));

      setMusicBaseQueue(baseQueue);
      closeVideoSession(videoSessionRef.current?.sessionId);
      setVideoSession(null);
      setTranscodeVersion(0);
      setPlaybackSession({
        activeMode: "music",
        isDockOpen: true,
        viewMode: "docked",
        queue: orderedQueue,
        queueIndex: nextIndex,
        shuffle: nextShuffle,
        repeatMode: nextRepeatMode,
      });
    },
    [activeMode, closeVideoSession, pauseAllMediaElements, repeatMode, shuffle],
  );

  const playMedia = useCallback(
    (item: MediaItem) => {
      if (item.type === "music") {
        playMusicCollection([item], item);
        return;
      }
      if (item.type === "movie") {
        playMovie(item);
        return;
      }
      playEpisode(item);
    },
    [playEpisode, playMovie, playMusicCollection],
  );

  const playNextInQueue = useCallback(() => {
    setPlaybackSession((current) => {
      if (!current || current.activeMode !== "music" || current.queue.length === 0) return current;
      const atLastItem = current.queueIndex >= current.queue.length - 1;
      if (!atLastItem) {
        return {
          ...current,
          queueIndex: current.queueIndex + 1,
          isDockOpen: true,
          viewMode: "docked",
        };
      }
      if (current.repeatMode === "all") {
        return { ...current, queueIndex: 0, isDockOpen: true, viewMode: "docked" };
      }
      return current;
    });
  }, []);

  const playPreviousInQueue = useCallback(() => {
    setPlaybackSession((current) => {
      if (!current || current.activeMode !== "music" || current.queue.length === 0) return current;
      if (current.queueIndex > 0) {
        return {
          ...current,
          queueIndex: current.queueIndex - 1,
          isDockOpen: true,
          viewMode: "docked",
        };
      }
      if (current.repeatMode === "all") {
        return {
          ...current,
          queueIndex: current.queue.length - 1,
          isDockOpen: true,
          viewMode: "docked",
        };
      }
      return current;
    });
  }, []);

  const toggleShuffle = useCallback(() => {
    setPlaybackSession((current) => {
      if (!current || current.activeMode !== "music") return current;
      const currentTrack = current.queue[current.queueIndex];
      if (!currentTrack || musicBaseQueue.length === 0) {
        return { ...current, shuffle: !current.shuffle };
      }
      const nextShuffle = !current.shuffle;
      const nextQueue = nextShuffle
        ? shuffleQueue(musicBaseQueue, currentTrack.id)
        : musicBaseQueue;
      return {
        ...current,
        shuffle: nextShuffle,
        queue: nextQueue,
        queueIndex: Math.max(0, indexOfQueueItem(nextQueue, currentTrack.id)),
      };
    });
  }, [musicBaseQueue]);

  const cycleRepeatMode = useCallback(() => {
    setPlaybackSession((current) => {
      if (!current || current.activeMode !== "music") return current;
      if (current.repeatMode === "off") return { ...current, repeatMode: "all" };
      if (current.repeatMode === "all") return { ...current, repeatMode: "one" };
      return { ...current, repeatMode: "off" };
    });
  }, []);

  const togglePlayPause = useCallback(() => {
    const active = getActiveMediaElement();
    if (!active) return;
    if (active.paused) {
      void active.play().catch(() => {});
      return;
    }
    active.pause();
  }, [getActiveMediaElement]);

  const seekTo = useCallback(
    (seconds: number) => {
      const active = getActiveMediaElement();
      if (!active) return;
      active.currentTime = Math.max(0, seconds);
    },
    [getActiveMediaElement],
  );

  const setMuted = useCallback((nextMuted: boolean) => {
    setMutedState(nextMuted);
  }, []);

  const setVolume = useCallback((nextVolume: number) => {
    const clamped = clampVolume(nextVolume);
    setVolumeState(clamped);
    if (clamped > 0) {
      setMutedState(false);
    }
  }, []);

  const enterFullscreen = useCallback(() => {
    if (activeMode !== "video" || !activeItem) return;
    setPlaybackSession((current) =>
      current ? { ...current, isDockOpen: true, viewMode: "fullscreen" } : current,
    );
  }, [activeItem, activeMode]);

  const exitFullscreen = useCallback(() => {
    exitBrowserFullscreen();
    setPlaybackSession((current) =>
      current && current.activeMode === "video"
        ? { ...current, isDockOpen: true, viewMode: "docked" }
        : current,
    );
  }, [exitBrowserFullscreen]);

  useEffect(() => {
    if (!BASE_URL) return;
    mountedRef.current = true;
    const connect = () => {
      if (!mountedRef.current) return;
      const ws = new WebSocket(buildPlumWebSocketUrl(BASE_URL, window.location.origin));
      wsRef.current = ws;
      ws.addEventListener("open", () => {
        if (mountedRef.current) setWsConnected(true);
      });
      ws.addEventListener("close", () => {
        if (!mountedRef.current) return;
        if (wsRef.current === ws) {
          wsRef.current = null;
        }
        setWsConnected(false);
        reconnectTimeoutRef.current = setTimeout(connect, 3000);
      });
      ws.addEventListener("message", (event) => {
        if (!mountedRef.current) return;
        const rawData = typeof event.data === "string" ? event.data : String(event.data);
        const data = parsePlumWebSocketEvent(rawData);
        if (!data) {
          setLastEvent(rawData);
          return;
        }
        const activeVideoItemId = activeVideoItemIdRef.current;
        if (data.type === "transcode_started") {
          if (activeVideoItemId == null || data.id !== activeVideoItemId) return;
          setLastEvent("Transcoding...");
        } else if (data.type === "transcode_complete") {
          if (activeVideoItemId == null || data.id !== activeVideoItemId) return;
          setLastEvent(data.success ? "Ready" : `Error: ${data.error || "Transcode failed"}`);
          if (data.success) {
            setTranscodeVersion((v) => v + 1);
          }
        } else if (data.type === "playback_session_update") {
          const currentSession = videoSessionRef.current;
          if (
            activeVideoItemId == null ||
            currentSession == null ||
            data.mediaId !== activeVideoItemId ||
            data.sessionId !== currentSession.sessionId
          ) {
            return;
          }
          if (data.status === "ready") {
            const shouldActivate = data.revision >= currentSession.desiredRevision;
            setVideoSession((current) =>
              current == null || current.sessionId !== data.sessionId
                ? current
                : {
                    ...current,
                    currentRevision: shouldActivate ? data.revision : current.currentRevision,
                    desiredRevision: Math.max(current.desiredRevision, data.revision),
                    audioIndex: data.audioIndex,
                    status: "ready",
                    playlistPath: data.playlistPath,
                    error: data.error ?? "",
                  },
            );
            if (shouldActivate) {
              setTranscodeVersion(data.revision);
              setLastEvent("Stream ready");
            }
          } else if (data.status === "error") {
            setVideoSession((current) =>
              current == null || current.sessionId !== data.sessionId
                ? current
                : {
                    ...current,
                    status: "error",
                    error: data.error ?? "Playback session failed",
                  },
            );
            setLastEvent(`Error: ${data.error || "Playback session failed"}`);
          } else if (data.status === "closed") {
            setVideoSession((current) => (current?.sessionId === data.sessionId ? null : current));
            setTranscodeVersion(0);
          } else {
            setLastEvent("Preparing stream...");
          }
        } else if (data.type === "transcode_warning") {
          if (activeVideoItemId == null || data.id !== activeVideoItemId) return;
          setLastEvent(data.warning);
        } else if (data.type === "welcome") {
          setLastEvent(data.message);
        } else {
          setLastEvent(data.type);
        }
      });
      ws.addEventListener("error", () => {});
    };
    // Defer the initial connection so Strict Mode cleanup can cancel it
    // without closing a socket that is still establishing.
    connectTimeoutRef.current = setTimeout(connect, 0);
    return () => {
      mountedRef.current = false;
      clearTimeout(connectTimeoutRef.current);
      connectTimeoutRef.current = 0;
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = 0;
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, []);

  const value = useMemo<PlayerContextValue>(
    () => ({
      playbackSession,
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
      playMedia,
      playMovie,
      playEpisode,
      playShowGroup,
      playMusicCollection,
      dismissDock,
      closePlayer: dismissDock,
      togglePlayPause,
      seekTo,
      setMuted,
      setVolume,
      enterFullscreen,
      exitFullscreen,
      registerMediaElement,
      playNextInQueue,
      playPreviousInQueue,
      toggleShuffle,
      cycleRepeatMode,
      changeAudioTrack,
      wsConnected,
      lastEvent,
    }),
    [
      playbackSession,
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
      playMedia,
      playMovie,
      playEpisode,
      playShowGroup,
      playMusicCollection,
      dismissDock,
      togglePlayPause,
      seekTo,
      setMuted,
      setVolume,
      enterFullscreen,
      exitFullscreen,
      registerMediaElement,
      playNextInQueue,
      playPreviousInQueue,
      toggleShuffle,
      cycleRepeatMode,
      changeAudioTrack,
      wsConnected,
      lastEvent,
    ],
  );

  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>;
}

export function usePlayer() {
  const ctx = useContext(PlayerContext);
  if (!ctx) throw new Error("usePlayer must be used within PlayerProvider");
  return ctx;
}
