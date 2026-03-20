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
import { playbackSessionPlaylistUrl } from "@plum/shared";
import type { MediaItem, PlaybackSession as ApiPlaybackSession } from "../api";
import {
  BASE_URL,
  type PlumWebSocketCommand,
  closePlaybackSession,
  createPlaybackSession,
  updatePlaybackSessionAudio,
} from "../api";
import { resolveLibraryPlaybackPreferences } from "../lib/playbackPreferences";
import {
  clampVolume,
  indexOfQueueItem,
  preferredInitialAudioIndex,
  shuffleQueue,
} from "../lib/playback/playerQueue";
import { sortMusicTracks } from "../lib/musicGrouping";
import { sortEpisodes } from "../lib/showGrouping";
import { useLibraries } from "../queries";
import { useWs } from "./WsContext";

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
  videoSourceUrl: string;
  playMedia: (item: MediaItem) => void;
  playMovie: (item: MediaItem) => void;
  playEpisode: (item: MediaItem) => void;
  playShowGroup: (items: MediaItem[], startItem?: MediaItem) => void;
  playMusicCollection: (items: MediaItem[], startItem?: MediaItem) => void;
  dismissDock: () => void;
  togglePlayPause: () => void;
  seekTo: (seconds: number) => void;
  setMuted: (muted: boolean) => void;
  setVolume: (volume: number) => void;
  enterFullscreen: () => void;
  exitFullscreen: () => void;
  registerMediaElement: (
    slot: MediaElementSlot,
    element: HTMLMediaElement | null,
  ) => void;
  playNextInQueue: () => void;
  playPreviousInQueue: () => void;
  toggleShuffle: () => void;
  cycleRepeatMode: () => void;
  changeAudioTrack: (audioIndex: number) => Promise<void>;
  wsConnected: boolean;
  lastEvent: string;
};

const PlayerContext = createContext<PlayerContextValue | null>(null);

export function PlayerProvider({ children }: { children: ReactNode }) {
  const { data: libraries = [] } = useLibraries();
  const [playbackSession, setPlaybackSession] =
    useState<PlaybackSession | null>(null);
  const [videoSession, setVideoSession] = useState<VideoSessionState | null>(
    null,
  );
  const [musicBaseQueue, setMusicBaseQueue] = useState<MediaItem[]>([]);
  const [volume, setVolumeState] = useState(1);
  const [muted, setMutedState] = useState(false);
  const [lastEvent, setLastEvent] = useState("");
  const mountedRef = useRef(true);
  const volumeRef = useRef(1);
  const mutedRef = useRef(false);
  const activeVideoItemIdRef = useRef<number | null>(null);
  const videoSessionRef = useRef<VideoSessionState | null>(null);
  const mediaElementsRef = useRef<
    Record<MediaElementSlot, HTMLMediaElement | null>
  >({
    audio: null,
    video: null,
  });
  const { wsConnected, latestEvent, eventSequence, sendCommand } = useWs();

  const activeItem = playbackSession?.queue[playbackSession.queueIndex] ?? null;
  const activeMode = playbackSession?.activeMode ?? null;
  const isDockOpen = playbackSession?.isDockOpen ?? false;
  const viewMode = playbackSession?.viewMode ?? "docked";
  const queue = useMemo(() => playbackSession?.queue ?? [], [playbackSession]);
  const queueIndex = playbackSession?.queueIndex ?? 0;
  const shuffle = playbackSession?.shuffle ?? false;
  const repeatMode = playbackSession?.repeatMode ?? "off";

  activeVideoItemIdRef.current =
    activeMode === "video" ? (activeItem?.id ?? null) : null;
  videoSessionRef.current = videoSession;

  const videoSourceUrl =
    activeMode === "video" &&
    videoSession != null &&
    videoSession.currentRevision > 0
      ? playbackSessionPlaylistUrl(
          BASE_URL,
          videoSession.sessionId,
          videoSession.currentRevision,
        )
      : "";

  const sendPlaybackCommand = useCallback(
    (command: PlumWebSocketCommand) => {
      sendCommand(command);
    },
    [sendCommand],
  );

  const closeVideoSession = useCallback(
    (sessionId?: string | null) => {
      if (!sessionId) return;
      sendPlaybackCommand({ action: "detach_playback_session", sessionId });
      closePlaybackSession(sessionId).catch(() => {});
    },
    [sendPlaybackCommand],
  );

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
    setLastEvent(
      session.status === "ready" ? "Stream ready" : "Preparing stream...",
    );
  }, []);

  useEffect(() => {
    if (!wsConnected) return;
    const sessionId = videoSession?.sessionId;
    if (!sessionId) return;
    sendPlaybackCommand({ action: "attach_playback_session", sessionId });
  }, [sendPlaybackCommand, videoSession?.sessionId, wsConnected]);

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
      element.volume = volumeRef.current;
      element.muted = mutedRef.current;
    },
    [],
  );

  useEffect(() => {
    volumeRef.current = volume;
    mutedRef.current = muted;
    for (const element of Object.values(mediaElementsRef.current)) {
      if (!element) continue;
      element.volume = volume;
      element.muted = muted;
    }
  }, [muted, volume]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

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
    setLastEvent("");
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
      setLastEvent("");
      if (!nextItem) return;
      const activeLibrary =
        libraries.find((library) => library.id === nextItem.library_id) ?? null;
      const preferredAudioLanguage = resolveLibraryPlaybackPreferences(
        activeLibrary ?? { type: nextItem.type },
      ).preferredAudioLanguage;
      createPlaybackSession(nextItem.id, {
        audioIndex: preferredInitialAudioIndex(
          nextItem,
          preferredAudioLanguage,
        ),
      })
        .then((session) => {
          if (!mountedRef.current) return;
          applyPlaybackSession(session);
        })
        .catch((err) => {
          console.error("[Player] createPlaybackSession failed", err);
          setLastEvent(
            `Error: ${err instanceof Error ? err.message : "Failed to start playback"}`,
          );
        });
    },
    [applyPlaybackSession, closeVideoSession, libraries, pauseAllMediaElements],
  );

  const changeAudioTrack = useCallback(
    async (audioIndex: number) => {
      const session = videoSessionRef.current;
      if (activeMode !== "video" || !activeItem || !session) return;
      setLastEvent("Switching audio track...");
      try {
        const nextSession = await updatePlaybackSessionAudio(
          session.sessionId,
          { audioIndex },
        );
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
      const baseQueue = sortMusicTracks(
        items.filter((item) => item.type === "music"),
      );
      if (baseQueue.length === 0) return;

      pauseAllMediaElements();

      const target = startItem ?? baseQueue[0];
      const nextShuffle = activeMode === "music" ? shuffle : false;
      const nextRepeatMode = activeMode === "music" ? repeatMode : "off";
      const orderedQueue = nextShuffle
        ? shuffleQueue(baseQueue, target.id)
        : baseQueue;
      const nextIndex = Math.max(0, indexOfQueueItem(orderedQueue, target.id));

      setMusicBaseQueue(baseQueue);
      closeVideoSession(videoSessionRef.current?.sessionId);
      setVideoSession(null);
      setLastEvent("");
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
      if (
        !current ||
        current.activeMode !== "music" ||
        current.queue.length === 0
      )
        return current;
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
        return {
          ...current,
          queueIndex: 0,
          isDockOpen: true,
          viewMode: "docked",
        };
      }
      return current;
    });
  }, []);

  const playPreviousInQueue = useCallback(() => {
    setPlaybackSession((current) => {
      if (
        !current ||
        current.activeMode !== "music" ||
        current.queue.length === 0
      )
        return current;
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
      if (current.repeatMode === "off")
        return { ...current, repeatMode: "all" };
      if (current.repeatMode === "all")
        return { ...current, repeatMode: "one" };
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
      current
        ? { ...current, isDockOpen: true, viewMode: "fullscreen" }
        : current,
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
    if (!latestEvent || latestEvent.type !== "playback_session_update") {
      return;
    }

    const activeVideoItemId = activeVideoItemIdRef.current;
    const currentSession = videoSessionRef.current;
    if (
      activeVideoItemId == null ||
      currentSession == null ||
      latestEvent.mediaId !== activeVideoItemId ||
      latestEvent.sessionId !== currentSession.sessionId
    ) {
      return;
    }

    if (latestEvent.status === "ready") {
      const shouldActivate =
        latestEvent.revision >= currentSession.desiredRevision;
      setVideoSession((current) =>
        current == null || current.sessionId !== latestEvent.sessionId
          ? current
          : {
              ...current,
              currentRevision: shouldActivate
                ? latestEvent.revision
                : current.currentRevision,
              desiredRevision: Math.max(
                current.desiredRevision,
                latestEvent.revision,
              ),
              audioIndex: latestEvent.audioIndex,
              status: "ready",
              playlistPath: latestEvent.playlistPath,
              error: latestEvent.error ?? "",
            },
      );
      if (shouldActivate) {
        setLastEvent("Stream ready");
      }
      return;
    }

    if (latestEvent.status === "error") {
      setVideoSession((current) =>
        current == null || current.sessionId !== latestEvent.sessionId
          ? current
          : {
              ...current,
              status: "error",
              error: latestEvent.error ?? "Playback session failed",
            },
      );
      setLastEvent(`Error: ${latestEvent.error || "Playback session failed"}`);
      return;
    }

    if (latestEvent.status === "closed") {
      setVideoSession((current) =>
        current?.sessionId === latestEvent.sessionId ? null : current,
      );
      setLastEvent("");
      return;
    }

    setLastEvent("Preparing stream...");
  }, [eventSequence, latestEvent]);

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

  return (
    <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>
  );
}

export function usePlayer() {
  const ctx = useContext(PlayerContext);
  if (!ctx) throw new Error("usePlayer must be used within PlayerProvider");
  return ctx;
}
