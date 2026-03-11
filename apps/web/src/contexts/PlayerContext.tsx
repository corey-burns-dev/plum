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
import { buildPlumWebSocketUrl, parsePlumWebSocketEvent } from "@plum/shared";
import type { MediaItem } from "../api";
import { BASE_URL, cancelTranscode, startTranscode } from "../api";
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
  playMedia: (item: MediaItem) => void;
  playMovie: (item: MediaItem) => void;
  playEpisode: (item: MediaItem) => void;
  playShowGroup: (items: MediaItem[]) => void;
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
  const [musicBaseQueue, setMusicBaseQueue] = useState<MediaItem[]>([]);
  const [volume, setVolumeState] = useState(1);
  const [muted, setMutedState] = useState(false);
  const [wsConnected, setWsConnected] = useState(false);
  const [lastEvent, setLastEvent] = useState("");
  const wsRef = useRef<WebSocket | null>(null);
  const connectTimeoutRef = useRef<ReturnType<typeof setTimeout>>(0);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout>>(0);
  const mountedRef = useRef(true);
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
      cancelTranscode().catch(() => {});
    }
    pauseAllMediaElements();
    exitBrowserFullscreen();
    setPlaybackSession(null);
    setMusicBaseQueue([]);
  }, [exitBrowserFullscreen, pauseAllMediaElements, playbackSession?.activeMode]);

  const playVideoQueue = useCallback(
    (items: MediaItem[], startIndex = 0) => {
      if (items.length === 0) return;
      pauseAllMediaElements();
      const clampedIndex = Math.max(0, Math.min(startIndex, items.length - 1));
      const nextItem = items[clampedIndex] ?? items[0];
      setPlaybackSession((current) => ({
        activeMode: "video",
        isDockOpen: true,
        viewMode: "docked",
        queue: items,
        queueIndex: clampedIndex,
        shuffle: false,
        repeatMode: current?.repeatMode ?? "off",
      }));
      setMusicBaseQueue([]);
      if (!nextItem) return;
      // Cancel any running transcode before starting a new one.
      cancelTranscode().catch(() => {});
      startTranscode(nextItem.id).catch((err) => {
        console.error("[Player] startTranscode failed", err);
        setLastEvent(`Error: ${err instanceof Error ? err.message : "Failed to start transcode"}`);
      });
    },
    [pauseAllMediaElements],
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
    (items: MediaItem[]) => {
      if (items.length === 0) return;
      const episodes = [...items];
      sortEpisodes(episodes);
      playVideoQueue(episodes);
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
    [activeMode, pauseAllMediaElements, repeatMode, shuffle],
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
    setPlaybackSession((current) => (current ? { ...current, viewMode: "docked" } : current));
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
        if (data.type === "transcode_started") {
          setLastEvent("Transcoding...");
        } else if (data.type === "transcode_complete") {
          setLastEvent(data.success ? "Ready" : `Error: ${data.error || "Transcode failed"}`);
        } else if (data.type === "transcode_warning") {
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
