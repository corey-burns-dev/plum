import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../api";
import { PlaybackDock } from "./PlaybackDock";

const mockUsePlayer = vi.fn();
const mockChangeAudioTrack = vi.fn();

vi.mock("../contexts/PlayerContext", () => ({
  usePlayer: () => mockUsePlayer(),
}));

vi.mock("hls.js", () => ({
  default: class MockHls {
    static isSupported() {
      return true;
    }

    loadSource() {}
    attachMedia() {}
    destroy() {}
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

describe("PlaybackDock audio track selection", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    mockChangeAudioTrack.mockReset();
    mockChangeAudioTrack.mockResolvedValue(undefined);
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
      transcodeVersion: 0,
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

  it("reloads the active video element when a new transcode completes", async () => {
    const loadSpy = vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
    const { container, queryClient, rerender } = renderDock();
    const video = container.querySelector("video") as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    if (!video) {
      throw new Error("Expected a video element");
    }

    let currentTime = 24;
    Object.defineProperty(video, "currentTime", {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    });

    mockUsePlayer.mockReturnValue({
      ...mockUsePlayer.mock.results.at(-1)?.value,
      transcodeVersion: 1,
      videoSourceUrl:
        "http://localhost:3000/api/playback/sessions/session-1/revisions/2/index.m3u8",
    });

    rerender(
      <QueryClientProvider client={queryClient}>
        <PlaybackDock />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(loadSpy).toHaveBeenCalledTimes(1);
      expect(pauseSpy).toHaveBeenCalled();
    });

    currentTime = 0;
    fireEvent.loadedMetadata(video);
    expect(currentTime).toBe(24);
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
      expect(mockChangeAudioTrack).toHaveBeenCalledWith(2);
    });
  });
});
