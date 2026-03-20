import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../api";
import {
  playerControlsAppearanceChangedEvent,
  playerControlsAppearanceStorageKey,
} from "../lib/playbackPreferences";
import { PlaybackDock } from "./PlaybackDock";

type MockHlsInstance = {
  handlers: Map<string, Array<(...args: unknown[]) => void>>;
  loadSource: ReturnType<typeof vi.fn>;
  attachMedia: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
  startLoad: ReturnType<typeof vi.fn>;
  recoverMediaError: ReturnType<typeof vi.fn>;
  on: (event: string, handler: (...args: unknown[]) => void) => void;
  emit: (event: string, ...args: unknown[]) => void;
};

type MockCue = {
  startTime: number;
  endTime: number;
  text: string;
  line?: number | string;
};

type MockTextTrack = {
  mode: TextTrackMode;
  cues: MockCue[];
  addCue: (cue: MockCue) => void;
  removeCue: (cue: MockCue) => void;
};

const mockUsePlayer = vi.fn();
const mockChangeAudioTrack = vi.fn();
const { mockHlsInstances } = vi.hoisted(() => ({
  mockHlsInstances: [] as MockHlsInstance[],
}));

vi.mock("../contexts/PlayerContext", () => ({
  usePlayer: () => mockUsePlayer(),
}));

vi.mock("hls.js", () => ({
  default: class {
    static Events = {
      MANIFEST_PARSED: "manifestParsed",
      ERROR: "error",
    };

    static ErrorTypes = {
      NETWORK_ERROR: "networkError",
      MEDIA_ERROR: "mediaError",
    };

    static isSupported() {
      return true;
    }

    handlers = new Map<string, Array<(...args: unknown[]) => void>>();
    loadSource = vi.fn();
    attachMedia = vi.fn();
    destroy = vi.fn();
    startLoad = vi.fn();
    recoverMediaError = vi.fn();

    constructor() {
      mockHlsInstances.push(this);
    }

    on(event: string, handler: (...args: unknown[]) => void) {
      const handlers = this.handlers.get(event) ?? [];
      handlers.push(handler);
      this.handlers.set(event, handlers);
    }

    emit(event: string, ...args: unknown[]) {
      for (const handler of this.handlers.get(event) ?? []) {
        handler(...args);
      }
    }
  },
}));

function renderDock() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <PlaybackDock />
      </QueryClientProvider>,
    ),
  };
}

function setVideoCurrentTime(video: HTMLVideoElement, currentTime: number) {
  Object.defineProperty(video, "currentTime", {
    configurable: true,
    value: currentTime,
    writable: true,
  });
}

describe("PlaybackDock audio track selection", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
    vi.spyOn(console, "error").mockImplementation(() => {});
    mockChangeAudioTrack.mockReset();
    mockChangeAudioTrack.mockResolvedValue(undefined);
    mockHlsInstances.length = 0;
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      {
        id: 7,
        name: "Anime",
        type: "anime",
        path: "/anime",
        user_id: 1,
        preferred_audio_language: "en",
        preferred_subtitle_language: "en",
        subtitles_enabled_by_default: true,
      },
    ]);
    vi.spyOn(api, "updateMediaProgress").mockResolvedValue();
    mockUsePlayer.mockReturnValue({
      activeItem: {
        id: 42,
        library_id: 7,
        title: "Track Test",
        path: "/movies/track-test.mkv",
        duration: 120,
        type: "movie",
        embeddedAudioTracks: [
          { streamIndex: 1, language: "eng", title: "English" },
          { streamIndex: 2, language: "jpn", title: "Japanese" },
        ],
      },
      activeMode: "video",
      isDockOpen: true,
      viewMode: "docked",
      queue: [],
      queueIndex: 0,
      shuffle: false,
      repeatMode: "off",
      volume: 1,
      muted: false,
      videoSourceUrl:
        "http://localhost:3000/api/playback/sessions/session-1/revisions/1/index.m3u8",
      wsConnected: false,
      lastEvent: "",
      registerMediaElement: vi.fn(),
      togglePlayPause: vi.fn(),
      seekTo: vi.fn(),
      setMuted: vi.fn(),
      setVolume: vi.fn(),
      enterFullscreen: vi.fn(),
      exitFullscreen: vi.fn(),
      dismissDock: vi.fn(),
      playNextInQueue: vi.fn(),
      playPreviousInQueue: vi.fn(),
      toggleShuffle: vi.fn(),
      cycleRepeatMode: vi.fn(),
      changeAudioTrack: mockChangeAudioTrack,
    });
  });

  it("switches audio tracks from the dock menu and requests a matching transcode", async () => {
    const { container } = renderDock();
    const video = container.querySelector("video") as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      throw new Error("Expected a video element");
    }

    const browserAudioTracks = [{ enabled: true }, { enabled: false }];
    Object.defineProperty(video, "audioTracks", {
      configurable: true,
      value: browserAudioTracks,
    });

    fireEvent.loadedMetadata(video);

    const audioButton = await screen.findByRole("button", { name: /Audio track:/i });
    fireEvent.click(audioButton);
    fireEvent.click(screen.getByRole("option", { name: /Japanese/i }));

    await waitFor(() => {
      expect(browserAudioTracks[0]?.enabled).toBe(false);
      expect(browserAudioTracks[1]?.enabled).toBe(true);
      expect(mockChangeAudioTrack).toHaveBeenCalledWith(2);
    });
  });

  it("reloads the active video element when the playback revision URL changes", async () => {
    const { queryClient, rerender } = renderDock();

    await waitFor(() => {
      expect(mockHlsInstances).toHaveLength(1);
    });

    mockUsePlayer.mockReturnValue({
      ...mockUsePlayer.mock.results.at(-1)?.value,
      videoSourceUrl:
        "http://localhost:3000/api/playback/sessions/session-1/revisions/2/index.m3u8",
    });

    rerender(
      <QueryClientProvider client={queryClient}>
        <PlaybackDock />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(mockHlsInstances).toHaveLength(2);
    });
    expect(mockHlsInstances[1]?.loadSource).toHaveBeenCalledWith(
      "http://localhost:3000/api/playback/sessions/session-1/revisions/2/index.m3u8",
    );
  });

  it("persists initial playback progress before the periodic interval elapses", async () => {
    const { container } = renderDock();
    const video = container.querySelector("video") as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      throw new Error("Expected a video element");
    }

    setVideoCurrentTime(video, 3);

    fireEvent.timeUpdate(video);

    await waitFor(() => {
      expect(api.updateMediaProgress).toHaveBeenCalledWith(42, {
        position_seconds: 3,
        duration_seconds: 120,
        completed: false,
      });
    });
  });

  it("persists playback position when switching from docked to fullscreen", async () => {
    const { container } = renderDock();
    const dockedVideo = container.querySelector("video") as HTMLVideoElement | null;
    expect(dockedVideo).toBeTruthy();
    if (!dockedVideo) {
      throw new Error("Expected a docked video element");
    }

    setVideoCurrentTime(dockedVideo, 37);
    fireEvent.timeUpdate(dockedVideo);
    vi.mocked(api.updateMediaProgress).mockClear();
    fireEvent.click(screen.getByRole("button", { name: /Open fullscreen player for/i }));

    await waitFor(() => {
      expect(api.updateMediaProgress).toHaveBeenCalledWith(42, {
        position_seconds: 37,
        duration_seconds: 120,
        completed: false,
      });
    });
  });

  it("persists playback position when switching from fullscreen to docked", async () => {
    mockUsePlayer.mockReturnValue({
      ...mockUsePlayer.mock.results.at(-1)?.value,
      viewMode: "fullscreen",
    });

    renderDock();
    const fullscreenPlayer = await screen.findByLabelText("Fullscreen video player");
    const fullscreenVideo = fullscreenPlayer.querySelector("video") as HTMLVideoElement | null;
    expect(fullscreenVideo).toBeTruthy();
    if (!fullscreenVideo) {
      throw new Error("Expected a fullscreen video element");
    }

    setVideoCurrentTime(fullscreenVideo, 52);
    fireEvent.timeUpdate(fullscreenVideo);
    vi.mocked(api.updateMediaProgress).mockClear();
    fireEvent.click(screen.getAllByRole("button", { name: /Return to docked player/i })[0]!);

    await waitFor(() => {
      expect(api.updateMediaProgress).toHaveBeenCalledWith(42, {
        position_seconds: 52,
        duration_seconds: 120,
        completed: false,
      });
    });
  });

  it("persists the latest playback position when closing the player", async () => {
    const { container } = renderDock();
    const video = container.querySelector("video") as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      throw new Error("Expected a video element");
    }

    setVideoCurrentTime(video, 41);
    fireEvent.timeUpdate(video);
    vi.mocked(api.updateMediaProgress).mockClear();

    fireEvent.click(screen.getByRole("button", { name: "Close player" }));

    await waitFor(() => {
      expect(api.updateMediaProgress).toHaveBeenCalledWith(42, {
        position_seconds: 41,
        duration_seconds: 120,
        completed: false,
      });
    });
  });

  it("keeps the active HLS attachment when mute state rerenders the player", async () => {
    const { queryClient, rerender } = renderDock();

    await waitFor(() => {
      expect(mockHlsInstances).toHaveLength(1);
    });

    const firstHls = mockHlsInstances[0];
    if (!firstHls) {
      throw new Error("Expected an HLS instance");
    }

    mockUsePlayer.mockReturnValue({
      ...mockUsePlayer.mock.results.at(-1)?.value,
      muted: true,
      registerMediaElement: vi.fn(),
    });

    rerender(
      <QueryClientProvider client={queryClient}>
        <PlaybackDock />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Unmute" })).toBeTruthy();
    });
    expect(mockHlsInstances).toHaveLength(1);
    expect(firstHls.destroy).not.toHaveBeenCalled();
  });

  it("reads and updates the player controls appearance preference", async () => {
    window.localStorage.setItem(playerControlsAppearanceStorageKey, "minimal");

    renderDock();

    const dock = screen.getByLabelText("Playback dock");
    expect(dock.className).toContain("playback-dock--controls-minimal");

    fireEvent.click(screen.getByRole("button", { name: "Subtitle settings" }));
    fireEvent.click(screen.getByRole("button", { name: "Default" }));

    await waitFor(() => {
      expect(window.localStorage.getItem(playerControlsAppearanceStorageKey)).toBe("default");
      expect(screen.getByLabelText("Playback dock").className).toContain(
        "playback-dock--controls-default",
      );
    });
  });

  it("syncs the player controls appearance when settings change it externally", async () => {
    window.localStorage.setItem(playerControlsAppearanceStorageKey, "minimal");
    renderDock();

    expect(screen.getByLabelText("Playback dock").className).toContain(
      "playback-dock--controls-minimal",
    );

    act(() => {
      window.localStorage.setItem(playerControlsAppearanceStorageKey, "default");
      window.dispatchEvent(
        new CustomEvent(playerControlsAppearanceChangedEvent, { detail: "default" }),
      );
    });

    await waitFor(() => {
      expect(screen.getByLabelText("Playback dock").className).toContain(
        "playback-dock--controls-default",
      );
    });
  });

  it("restarts loading on fatal HLS network errors", async () => {
    renderDock();

    await waitFor(() => {
      expect(mockHlsInstances).toHaveLength(1);
    });

    const hls = mockHlsInstances[0];
    if (!hls) {
      throw new Error("Expected an HLS instance");
    }

    act(() => {
      hls.emit("error", "error", {
        fatal: true,
        type: "networkError",
        details: "fragLoadError",
      });
    });

    expect(hls.startLoad).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(screen.getByText("Reconnecting stream...")).toBeTruthy();
    });
  });

  it("recovers media playback on fatal HLS media errors", async () => {
    renderDock();

    await waitFor(() => {
      expect(mockHlsInstances).toHaveLength(1);
    });

    const hls = mockHlsInstances[0];
    if (!hls) {
      throw new Error("Expected an HLS instance");
    }

    act(() => {
      hls.emit("error", "error", {
        fatal: true,
        type: "mediaError",
        details: "bufferStalledError",
      });
    });

    expect(hls.recoverMediaError).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(screen.getByText("Recovering playback...")).toBeTruthy();
    });
  });

  it("shows a stream error when the HLS failure is fatal and unrecoverable", async () => {
    renderDock();

    await waitFor(() => {
      expect(mockHlsInstances).toHaveLength(1);
    });

    const hls = mockHlsInstances[0];
    if (!hls) {
      throw new Error("Expected an HLS instance");
    }

    act(() => {
      hls.emit("error", "error", {
        fatal: true,
        type: "otherFatalError",
        details: "appendError",
      });
    });

    await waitFor(() => {
      expect(screen.getByText("Stream error: appendError")).toBeTruthy();
    });
  });

  it("prefers the library default audio language when available", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      {
        id: 7,
        name: "Anime",
        type: "anime",
        path: "/anime",
        user_id: 1,
        preferred_audio_language: "ja",
        preferred_subtitle_language: "en",
        subtitles_enabled_by_default: true,
      },
    ]);
    const { container } = renderDock();
    const video = container.querySelector("video") as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      throw new Error("Expected a video element");
    }

    const browserAudioTracks = [{ enabled: true }, { enabled: false }];
    Object.defineProperty(video, "audioTracks", {
      configurable: true,
      value: browserAudioTracks,
    });

    fireEvent.loadedMetadata(video);

    await waitFor(() => {
      expect(browserAudioTracks[0]?.enabled).toBe(false);
      expect(browserAudioTracks[1]?.enabled).toBe(true);
    });
    expect(mockChangeAudioTrack).not.toHaveBeenCalled();
  });

  it("reapplies the default subtitle track when media becomes ready", async () => {
    const originalCue = window.VTTCue;
    const originalAddTextTrack = HTMLMediaElement.prototype.addTextTrack;
    const originalTextTracksDescriptor = Object.getOwnPropertyDescriptor(
      HTMLMediaElement.prototype,
      "textTracks",
    );
    const tracksByElement = new WeakMap<HTMLMediaElement, MockTextTrack[]>();

    const createTrack = (): MockTextTrack => {
      const cues: MockCue[] = [];
      return {
        mode: "disabled",
        cues,
        addCue: (cue) => {
          cues.push(cue);
        },
        removeCue: (cue) => {
          const index = cues.indexOf(cue);
          if (index >= 0) {
            cues.splice(index, 1);
          }
        },
      };
    };

    const addTextTrackMock = vi.fn(function (this: HTMLMediaElement) {
      const track = createTrack();
      const tracks = tracksByElement.get(this) ?? [];
      tracks.push(track);
      tracksByElement.set(this, tracks);
      return track as unknown as TextTrack;
    });

    Object.defineProperty(window, "VTTCue", {
      configurable: true,
      writable: true,
      value: class {
        startTime: number;
        endTime: number;
        text: string;

        constructor(startTime: number, endTime: number, text: string) {
          this.startTime = startTime;
          this.endTime = endTime;
          this.text = text;
        }
      },
    });
    Object.defineProperty(HTMLMediaElement.prototype, "addTextTrack", {
      configurable: true,
      value: addTextTrackMock,
    });
    Object.defineProperty(HTMLMediaElement.prototype, "textTracks", {
      configurable: true,
      get() {
        return tracksByElement.get(this) ?? [];
      },
    });

    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(
        new Response("WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello world\n", { status: 200 }),
      );

    mockUsePlayer.mockReturnValue({
      ...mockUsePlayer.mock.results.at(-1)?.value,
      activeItem: {
        id: 42,
        library_id: 7,
        title: "Track Test",
        path: "/movies/track-test.mkv",
        duration: 120,
        type: "movie",
        subtitles: [{ id: 9, language: "eng", title: "English", format: "vtt" }],
        embeddedAudioTracks: [
          { streamIndex: 1, language: "eng", title: "English" },
          { streamIndex: 2, language: "jpn", title: "Japanese" },
        ],
      },
    });

    try {
      const { container } = renderDock();
      const video = container.querySelector("video") as HTMLVideoElement | null;
      expect(video).toBeTruthy();
      if (!video) {
        throw new Error("Expected a video element");
      }

      await waitFor(() => {
        expect(fetchSpy).toHaveBeenCalledTimes(1);
        expect(addTextTrackMock).toHaveBeenCalledTimes(1);
      });

      await waitFor(() => {
        const currentTracks = tracksByElement.get(video) ?? [];
        expect(currentTracks[0]?.mode).toBe("showing");
        expect(currentTracks[0]?.cues).toHaveLength(1);
      });

      tracksByElement.set(video, []);
      fireEvent.loadedMetadata(video);

      await waitFor(() => {
        expect(addTextTrackMock).toHaveBeenCalledTimes(2);
      });

      const recreatedTracks = tracksByElement.get(video) ?? [];
      expect(recreatedTracks).toHaveLength(1);
      expect(recreatedTracks[0]?.mode).toBe("showing");
      expect(recreatedTracks[0]?.cues).toHaveLength(1);
    } finally {
      fetchSpy.mockRestore();
      if (originalCue == null) {
        Reflect.deleteProperty(window as Window & { VTTCue?: typeof window.VTTCue }, "VTTCue");
      } else {
        Object.defineProperty(window, "VTTCue", {
          configurable: true,
          writable: true,
          value: originalCue,
        });
      }
      if (originalTextTracksDescriptor) {
        Object.defineProperty(
          HTMLMediaElement.prototype,
          "textTracks",
          originalTextTracksDescriptor,
        );
      } else {
        Reflect.deleteProperty(
          HTMLMediaElement.prototype as HTMLMediaElement & { textTracks?: TextTrackList },
          "textTracks",
        );
      }
      if (originalAddTextTrack) {
        Object.defineProperty(HTMLMediaElement.prototype, "addTextTrack", {
          configurable: true,
          value: originalAddTextTrack,
        });
      } else {
        Reflect.deleteProperty(
          HTMLMediaElement.prototype as HTMLMediaElement & {
            addTextTrack?: HTMLMediaElement["addTextTrack"];
          },
          "addTextTrack",
        );
      }
    }
  });
});
