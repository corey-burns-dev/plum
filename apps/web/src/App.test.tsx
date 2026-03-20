import { Effect } from "effect";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { loadAuthSessionEffect } from "@plum/shared";
import * as api from "./api";
import App from "./App";
import { IdentifyShowDialog } from "./components/IdentifyShowDialog";
import * as IdentifyQueueContext from "./contexts/IdentifyQueueContext";

vi.mock("@plum/shared", async () => {
  const actual = await vi.importActual<typeof import("@plum/shared")>("@plum/shared");
  const webApi = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    loadAuthSessionEffect: vi.fn(),
    runIdentifyLibraryTask: vi.fn(
      (_client: unknown, options: { libraryId: number; signal?: AbortSignal; timeoutMs: number }) =>
        webApi.identifyLibrary(options.libraryId, { signal: options.signal }),
    ),
  };
});

const defaultUser = {
  id: 1,
  email: "test@test.com",
  is_admin: true,
} satisfies api.User;

const defaultTranscodingSettings = {
  vaapiEnabled: false,
  vaapiDevicePath: "/dev/dri/renderD128",
  decodeCodecs: {
    h264: true,
    hevc: true,
    mpeg2: true,
    vc1: true,
    vp8: true,
    vp9: true,
    av1: true,
    hevc10bit: true,
    vp910bit: true,
  },
  hardwareEncodingEnabled: false,
  encodeFormats: {
    h264: true,
    hevc: false,
    av1: false,
  },
  preferredHardwareEncodeFormat: "h264",
  allowSoftwareFallback: true,
  crf: 23,
  audioBitrate: "192k",
  audioChannels: 2,
  threads: 0,
  keyframeInterval: 48,
  maxBitrate: "0",
} satisfies api.TranscodingSettings;

function mockAuthSession({
  hasAdmin = true,
  user = defaultUser,
}: {
  hasAdmin?: boolean;
  user?: api.User | null;
} = {}) {
  vi.mocked(loadAuthSessionEffect).mockReturnValue(
    Effect.succeed({
      hasAdmin,
      user,
    }),
  );
}

function renderApp() {
  return render(<App />);
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function identifyLibraryIds() {
  const identifyLibraryMock = api.identifyLibrary as typeof api.identifyLibrary & {
    mock: { calls: Array<[number]> };
  };
  return identifyLibraryMock.mock.calls.map((call) => call[0]);
}

describe("App library and player wiring", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.mocked(loadAuthSessionEffect).mockReset();
    window.history.pushState({}, "", "/library/1");
    mockAuthSession();
    vi.spyOn(api, "getSetupStatus").mockResolvedValue({ hasAdmin: true });
    vi.spyOn(api, "getMe").mockResolvedValue(defaultUser);
    vi.spyOn(api, "getLibraryScanStatus").mockImplementation(async (libraryId) => ({
      libraryId,
      phase: "idle",
      enriching: false,
      identifyPhase: "idle",
      identified: 0,
      identifyFailed: 0,
      processed: 0,
      added: 0,
      updated: 0,
      removed: 0,
      unmatched: 0,
      skipped: 0,
      identifyRequested: false,
      estimatedItems: 0,
      queuePosition: 0,
    }));
    vi.spyOn(api, "startLibraryScan").mockImplementation(async (libraryId) => ({
      libraryId,
      phase: "queued",
      enriching: false,
      identifyPhase: "idle",
      identified: 0,
      identifyFailed: 0,
      processed: 0,
      added: 0,
      updated: 0,
      removed: 0,
      unmatched: 0,
      skipped: 0,
      identifyRequested: false,
      estimatedItems: 0,
      queuePosition: 1,
      startedAt: new Date().toISOString(),
    }));
    vi.spyOn(api, "identifyLibrary").mockResolvedValue({ identified: 0, failed: 0 });
    vi.spyOn(api, "confirmShow").mockResolvedValue({ updated: 1 });
    vi.spyOn(api, "getHomeDashboard").mockResolvedValue({ continueWatching: [] });
    vi.spyOn(api, "createPlaybackSession").mockImplementation(async (mediaId, payload) => ({
      sessionId: `session-${mediaId}`,
      mediaId,
      revision: 1,
      audioIndex: payload?.audioIndex ?? -1,
      status: "starting",
      playlistPath: `/api/playback/sessions/session-${mediaId}/revisions/1/index.m3u8`,
    }));
    vi.spyOn(api, "updatePlaybackSessionAudio").mockImplementation(async (sessionId, payload) => ({
      sessionId,
      mediaId: Number(sessionId.replace("session-", "")) || 0,
      revision: 2,
      audioIndex: payload.audioIndex,
      status: "starting",
      playlistPath: `/api/playback/sessions/${sessionId}/revisions/2/index.m3u8`,
    }));
    vi.spyOn(api, "closePlaybackSession").mockResolvedValue();
  });

  it("renders library tab and show cards when TV library has media", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 42,
        title: "Test Show - S01E01 - Pilot",
        path: "/tv/TestShow/S01E01.mkv",
        duration: 1800,
        type: "tv",
        tmdb_id: 100,
        poster_path: "/poster.jpg",
        season: 1,
        episode: 1,
      },
    ]);

    renderApp();

    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledWith(1);
    });

    expect(await screen.findByRole("link", { name: /TV/i })).toBeTruthy();
    expect(await screen.findByText("Test Show")).toBeTruthy();
  });

  it("renders movie library as poster cards", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Die My Love",
        path: "/movies/Die My Love (2025)/Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio.mp4",
        duration: 7200,
        type: "movie",
        poster_path: "/poster.jpg",
        release_date: "2025-01-01",
      },
    ]);

    renderApp();

    const movieCard = await screen.findByRole("button", { name: /^Die My Love$/i });
    expect(movieCard).toBeTruthy();
    expect(screen.getByText(/2025/)).toBeTruthy();
  });

  it("plays a movie from the poster overlay and opens the fullscreen overlay from the dock surface", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Die My Love",
        path: "/movies/Die My Love (2025)/Die My Love.mp4",
        duration: 7200,
        type: "movie",
        poster_path: "/poster.jpg",
        backdrop_path: "/backdrop.jpg",
        release_date: "2025-01-01",
      },
    ]);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: /Play Die My Love/i }));

    expect(api.createPlaybackSession).toHaveBeenCalledWith(99, { audioIndex: -1 });
    expect(await screen.findByLabelText("Fullscreen video player")).toBeTruthy();

    fireEvent.click(screen.getAllByRole("button", { name: /Return to docked player/i })[0]!);

    expect(await screen.findByLabelText("Playback dock")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: /Open fullscreen player for Die My Love/i }),
    );

    expect(await screen.findByLabelText("Fullscreen video player")).toBeTruthy();
  });

  it("plays the surfaced continue-watching show directly from the dashboard", async () => {
    window.history.pushState({}, "", "/");
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "getHomeDashboard").mockResolvedValue({
      continueWatching: [
        {
          kind: "show",
          show_title: "Space Show",
          show_key: "tmdb-101",
          episode_label: "S02E04",
          remaining_seconds: 1200,
          media: {
            id: 55,
            library_id: 1,
            title: "Space Show - S02E04 - Echoes",
            path: "/tv/Space Show/S02E04.mkv",
            duration: 1800,
            type: "tv",
            season: 2,
            episode: 4,
            progress_percent: 33,
          },
        },
      ],
    });

    renderApp();

    const card = await screen.findByRole("button", { name: /^Space Show$/i });
    fireEvent.click(card);

    await waitFor(() => {
      expect(api.createPlaybackSession).toHaveBeenCalledWith(55, expect.anything());
    });
  });

  it("reveals hard TV cards as searching once easier matches appear", async () => {
    const identifyRequest = deferred<{ identified: number; failed: number }>();

    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 42,
        title: "Searching Show - S01E01 - Pilot",
        path: "/tv/Searching Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
      {
        id: 99,
        title: "Matched Show - S01E01 - Pilot",
        path: "/tv/Matched Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "identified",
        tmdb_id: 200,
        poster_path: "/poster.jpg",
        season: 1,
        episode: 1,
      },
    ]);
    vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise);

    renderApp();

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.identifyLibrary).mock.calls[0]?.[0]).toBe(1);

    expect(await screen.findByRole("link", { name: /Matched Show/i })).toBeTruthy();
    const searchingCard = await screen.findByRole("link", { name: /Searching Show/i });
    expect(within(searchingCard.closest(".show-card")!).getByText("Searching…")).toBeVisible();
  });

  it("shows movie cards as searching while identify is still in progress", async () => {
    const identifyRequest = deferred<{ identified: number; failed: number }>();

    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Die My Love",
        path: "/movies/Die My Love (2025)/Die My Love.mp4",
        duration: 7200,
        type: "movie",
        match_status: "unmatched",
      },
    ]);
    vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise);

    renderApp();

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(api.identifyLibrary).mock.calls[0]?.[0]).toBe(2);

    const movieCard = await screen.findByRole("button", { name: /^Die My Love$/i });
    expect(within(movieCard.closest(".show-card")!).getByText("Searching…")).toBeVisible();
  });

  it("polls active library media and reveals movie cards once metadata lands", async () => {
    vi.useFakeTimers();

    try {
      const identifyRequest = deferred<{ identified: number; failed: number }>();

      vi.spyOn(api, "listLibraries").mockResolvedValue([
        { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
      ]);
      vi.spyOn(api, "fetchLibraryMedia")
        .mockResolvedValueOnce([
          {
            id: 99,
            title: "Die My Love",
            path: "/movies/Die My Love (2025)/Die My Love.mp4",
            duration: 7200,
            type: "movie",
            match_status: "unmatched",
          },
        ])
        .mockResolvedValueOnce([
          {
            id: 99,
            title: "Die My Love",
            path: "/movies/Die My Love (2025)/Die My Love.mp4",
            duration: 7200,
            type: "movie",
            match_status: "identified",
            poster_path: "/poster-identified.jpg",
            release_date: "2025-01-01",
          },
        ]);
      vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise);

      await act(async () => {
        renderApp();
        await vi.advanceTimersByTimeAsync(1);
        await Promise.resolve();
        await Promise.resolve();
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(screen.getByRole("button", { name: /^Die My Love$/i })).toBeTruthy();
      expect(screen.queryByText("Identifying library…")).not.toBeInTheDocument();

      await act(async () => {
        identifyRequest.resolve({ identified: 1, failed: 0 });
        await Promise.resolve();
      });
    } finally {
      vi.useRealTimers();
    }
  }, 10_000);

  it("runs background identify for non-music libraries concurrently", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
      { id: 4, name: "Anime", type: "anime", path: "/anime", user_id: 1 },
      { id: 3, name: "Music", type: "music", path: "/music", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([]);
    const firstIdentify = deferred<{ identified: number; failed: number }>();
    const secondIdentify = deferred<{ identified: number; failed: number }>();
    const thirdIdentify = deferred<{ identified: number; failed: number }>();
    vi.mocked(api.identifyLibrary)
      .mockImplementationOnce(() => firstIdentify.promise)
      .mockImplementationOnce(() => secondIdentify.promise)
      .mockImplementationOnce(() => thirdIdentify.promise);

    renderApp();

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(3);
    });
    expect(identifyLibraryIds()).toEqual(expect.arrayContaining([1, 2, 4]));
    expect(identifyLibraryIds()).not.toContain(3);

    await act(async () => {
      firstIdentify.resolve({ identified: 1, failed: 0 });
      secondIdentify.resolve({ identified: 1, failed: 0 });
      thirdIdentify.resolve({ identified: 1, failed: 0 });
      await Promise.resolve();
    });
  });

  it("times out a hung identify request without blocking the next library", async () => {
    vi.useFakeTimers();

    try {
      vi.spyOn(api, "listLibraries").mockResolvedValue([
        { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
        { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
      ]);
      vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([]);
      vi.mocked(api.identifyLibrary)
        .mockImplementationOnce(
          (_libraryId, options) =>
            new Promise((_resolve, reject) => {
              options?.signal?.addEventListener("abort", () => {
                reject(new DOMException("Aborted", "AbortError"));
              });
            }),
        )
        .mockResolvedValueOnce({ identified: 0, failed: 0 });

      await act(async () => {
        renderApp();
        await vi.advanceTimersByTimeAsync(1);
        await Promise.resolve();
        await Promise.resolve();
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
        await Promise.resolve();
      });
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2);
      expect(identifyLibraryIds()).toEqual([1, 2]);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(180_000);
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2);
      expect(identifyLibraryIds()).toEqual([1, 2]);
    } finally {
      vi.useRealTimers();
    }
  }, 10_000);

  it("navigates to show detail and shows episode list with Play", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 42,
        title: "Test Show - S01E01 - Pilot",
        path: "/tv/TestShow/S01E01.mkv",
        duration: 1800,
        type: "tv",
        tmdb_id: 100,
        poster_path: "/poster.jpg",
        season: 1,
        episode: 1,
      },
    ]);

    renderApp();

    await screen.findByText("Test Show");
    fireEvent.click(screen.getByRole("link", { name: /Test Show/i }));

    expect(await screen.findByRole("link", { name: /Back to library/i })).toBeTruthy();
    const playButton = await screen.findByRole("button", { name: /Play/i });
    fireEvent.click(playButton);
    expect(api.createPlaybackSession).toHaveBeenCalledWith(42, { audioIndex: -1 });
    expect(await screen.findByLabelText("Fullscreen video player")).toBeTruthy();
  });

  it("plays the first episode in a TV group from the poster overlay", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 100,
        title: "Grouped Show - S01E02 - Second",
        path: "/tv/Grouped Show/S01E02.mkv",
        duration: 1800,
        type: "tv",
        tmdb_id: 100,
        poster_path: "/poster.jpg",
        season: 1,
        episode: 2,
      },
      {
        id: 99,
        title: "Grouped Show - S01E01 - First",
        path: "/tv/Grouped Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        tmdb_id: 100,
        poster_path: "/poster.jpg",
        season: 1,
        episode: 1,
      },
    ]);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: /Play Grouped Show/i }));

    expect(api.createPlaybackSession).toHaveBeenCalledWith(99, { audioIndex: -1 });
    expect(await screen.findByLabelText("Fullscreen video player")).toBeTruthy();
  });

  it("renders music sections and opens the bottom player without transcoding", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 3, name: "Music", type: "music", path: "/music", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 11,
        title: "Track One",
        path: "/music/Artist/Album/01 - Track One.flac",
        duration: 245,
        type: "music",
        artist: "Artist",
        album: "Album",
        album_artist: "Artist",
        track_number: 1,
      },
      {
        id: 12,
        title: "Track Two",
        path: "/music/Artist/Album/02 - Track Two.flac",
        duration: 255,
        type: "music",
        artist: "Artist",
        album: "Album",
        album_artist: "Artist",
        track_number: 2,
      },
    ]);

    renderApp();

    expect(await screen.findByText("Tracks")).toBeTruthy();
    expect(screen.getByText("Albums")).toBeTruthy();
    expect(screen.getByText("Artists")).toBeTruthy();
    expect(screen.getByText("Genres")).toBeTruthy();
    expect(screen.getByText("Playlists")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Track One/i }));

    expect(await screen.findByLabelText("Music player")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Enable shuffle/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Previous track/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Next track/i })).toBeTruthy();
    expect(api.createPlaybackSession).not.toHaveBeenCalled();
  });

  it("plays a music album from the poster overlay without rendering video controls", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 3, name: "Music", type: "music", path: "/music", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 11,
        title: "Track One",
        path: "/music/Artist/Album/01 - Track One.flac",
        duration: 245,
        type: "music",
        artist: "Artist",
        album: "Album",
        album_artist: "Artist",
        track_number: 1,
        poster_path: "/album.jpg",
      },
      {
        id: 12,
        title: "Track Two",
        path: "/music/Artist/Album/02 - Track Two.flac",
        duration: 255,
        type: "music",
        artist: "Artist",
        album: "Album",
        album_artist: "Artist",
        track_number: 2,
        poster_path: "/album.jpg",
      },
    ]);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: /Play Album/i }));

    expect(await screen.findByLabelText("Music player")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: /Open fullscreen player/i }),
    ).not.toBeInTheDocument();
    expect(api.createPlaybackSession).not.toHaveBeenCalled();
  });

  it("retries auto-identify after a failed first attempt", async () => {
    const firstIdentify = deferred<{ identified: number; failed: number }>();
    const secondIdentify = deferred<{ identified: number; failed: number }>();

    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia")
      .mockResolvedValueOnce([
        {
          id: 42,
          title: "Retry Show - S01E01 - Pilot",
          path: "/tv/Retry Show/S01E01.mkv",
          duration: 1800,
          type: "tv",
          match_status: "local",
          season: 1,
          episode: 1,
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 42,
          title: "Retry Show - S01E01 - Pilot",
          path: "/tv/Retry Show/S01E01.mkv",
          duration: 1800,
          type: "tv",
          match_status: "identified",
          tmdb_id: 100,
          poster_path: "/poster.jpg",
          season: 1,
          episode: 1,
        },
      ]);
    vi.mocked(api.identifyLibrary)
      .mockImplementationOnce(() => firstIdentify.promise)
      .mockImplementationOnce(() => secondIdentify.promise);

    renderApp();

    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledWith(1);
    });
    const retryCard = await screen.findByRole("link", { name: /Retry Show/i });
    expect(within(retryCard.closest(".show-card")!).getByText("Searching…")).toBeVisible();

    firstIdentify.reject(new Error("temporary failure"));

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2);
    });

    secondIdentify.resolve({ identified: 1, failed: 0 });

    await waitFor(() => {
      expect(api.fetchLibraryMedia).toHaveBeenCalledTimes(2);
    });
    expect(await screen.findByRole("link", { name: /Retry Show/i })).toBeTruthy();
  });

  it("shows sidebar activity, soft reveal, and hard-timeout failure for deferred cards", async () => {
    vi.useFakeTimers();

    try {
      vi.spyOn(api, "listLibraries").mockResolvedValue([
        { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
      ]);
      vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
        {
          id: 42,
          title: "Missing Show - S01E01 - Pilot",
          path: "/tv/Missing Show/S01E01.mkv",
          duration: 1800,
          type: "tv",
          match_status: "local",
          season: 1,
          episode: 1,
        },
      ]);
      vi.spyOn(api, "searchSeries").mockResolvedValue([]);
      vi.mocked(api.identifyLibrary).mockImplementation(
        (_libraryId, options) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener("abort", () => {
              reject(new DOMException("Aborted", "AbortError"));
            });
          }),
      );

      await act(async () => {
        renderApp();
        await Promise.resolve();
        await Promise.resolve();
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(90_000);
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(screen.getByTestId("library-identifying-1")).toBeTruthy();
      const softRevealCard = screen.getByRole("link", { name: /Missing Show/i });
      expect(within(softRevealCard.closest(".show-card")!).getByText("Searching…")).toBeVisible();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(90_000);
        await Promise.resolve();
        await Promise.resolve();
      });

      expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Identify manually/i })).not.toBeInTheDocument();
      expect(within(softRevealCard.closest(".show-card")!).getByText("Searching…")).toBeVisible();
    } finally {
      vi.useRealTimers();
    }
  }, 10_000);

  it("shows a terminal TV failure state with manual identify action", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 42,
        title: "Missing Show - S01E01 - Pilot",
        path: "/tv/Missing Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
    ]);
    vi.spyOn(api, "searchSeries").mockResolvedValue([]);
    vi.mocked(api.identifyLibrary).mockResolvedValue({ identified: 0, failed: 1 });

    renderApp();

    expect(await screen.findByText("Couldn't match automatically")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Identify manually/i }));

    expect(await screen.findByRole("heading", { name: /Identify show/i })).toBeTruthy();
  });

  it("does not show manual identify while backend identify is still active", async () => {
    vi.spyOn(api, "getLibraryScanStatus").mockResolvedValue({
      libraryId: 1,
      phase: "completed",
      enriching: false,
      identifyPhase: "identifying",
      identified: 0,
      identifyFailed: 0,
      processed: 1,
      added: 1,
      updated: 0,
      removed: 0,
      unmatched: 1,
      skipped: 0,
      identifyRequested: true,
      estimatedItems: 1,
      queuePosition: 0,
    });
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 42,
        title: "Missing Show - S01E01 - Pilot",
        path: "/tv/Missing Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
    ]);

    renderApp();

    const searchingCard = await screen.findByRole("link", { name: /Missing Show/i });
    expect(within(searchingCard.closest(".show-card")!).getByText("Searching…")).toBeVisible();
    expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Identify manually/i })).not.toBeInTheDocument();
    expect(api.identifyLibrary).not.toHaveBeenCalled();
  });

  it("shows a review prompt for TV fallback matches and confirms it", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    const fetchLibraryMedia = vi.spyOn(api, "fetchLibraryMedia");
    fetchLibraryMedia.mockResolvedValue([
      {
        id: 42,
        title: "Slow Horses - S01E01 - Failure's Contagious",
        path: "/tv/Slow Horses/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "identified",
        tmdb_id: 321,
        poster_path: "/slow-horses.jpg",
        metadata_review_needed: false,
        season: 1,
        episode: 1,
      },
    ]);
    fetchLibraryMedia.mockResolvedValueOnce([
      {
        id: 42,
        title: "Slow Horses - S01E01 - Failure's Contagious",
        path: "/tv/Slow Horses/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "identified",
        tmdb_id: 321,
        poster_path: "/slow-horses.jpg",
        metadata_review_needed: true,
        season: 1,
        episode: 1,
      },
    ]);
    const identifyRequest = deferred<{ identified: number; failed: number }>();
    vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise);

    renderApp();

    expect(await screen.findByText("Is this correct?")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Confirm/i }));

    await waitFor(() => {
      expect(api.confirmShow).toHaveBeenCalledWith(1, { showKey: "tmdb-321" });
    });
    await waitFor(() => {
      expect(screen.queryByText("Is this correct?")).not.toBeInTheDocument();
    });
    await act(async () => {
      identifyRequest.resolve({ identified: 1, failed: 0 });
      await Promise.resolve();
    });
  });

  it("applies manual identify for unmatched TV shows and restarts library identify", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    const fetchLibraryMedia = vi.spyOn(api, "fetchLibraryMedia");
    fetchLibraryMedia.mockResolvedValueOnce([
      {
        id: 42,
        title: "Missing Show (2024) - S01E01 - Pilot",
        path: "/tv/Missing Show (2024)/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
    ]);
    fetchLibraryMedia.mockResolvedValueOnce([
      {
        id: 42,
        title: "Missing Show (2024) - S01E01 - Pilot",
        path: "/tv/Missing Show (2024)/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
    ]);
    fetchLibraryMedia.mockResolvedValue([
      {
        id: 42,
        title: "Correct Show - S01E01 - Pilot",
        path: "/tv/Missing Show (2024)/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "identified",
        tmdb_id: 456,
        poster_path: "/correct-show.jpg",
        season: 1,
        episode: 1,
      },
    ]);
    vi.spyOn(api, "searchSeries").mockResolvedValue([
      {
        Title: "Correct Show",
        Overview: "",
        ExternalID: "456",
        Provider: "tmdb",
        PosterURL: "/correct-show.jpg",
        BackdropURL: "",
        ReleaseDate: "2024-01-01",
        VoteAverage: 0,
      },
    ]);
    vi.spyOn(api, "identifyShow").mockResolvedValue({ updated: 1 });
    vi.mocked(api.identifyLibrary).mockResolvedValue({ identified: 0, failed: 1 });

    renderApp();

    expect(await screen.findByText("Couldn't match automatically")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Identify manually/i }));

    expect(await screen.findByRole("heading", { name: /Identify show/i })).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: /Choose/i }));

    await waitFor(() => {
      expect(api.identifyShow).toHaveBeenCalledWith(1, "title-missingshow2024", 456);
    });
    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.queryByRole("heading", { name: /Identify show/i })).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
    });
    expect(identifyLibraryIds()).toEqual([1, 1]);
  });

  it("queues a restarted identify pass with abortActive after a manual show match", async () => {
    const queueLibraryIdentify = vi.fn();
    const onSuccess = vi.fn();
    const onOpenChange = vi.fn();

    vi.spyOn(IdentifyQueueContext, "useIdentifyQueue").mockReturnValue({
      identifyPhases: {},
      getLibraryPhase: () => undefined,
      queueLibraryIdentify,
    });
    vi.spyOn(api, "searchSeries").mockResolvedValue([
      {
        Title: "Correct Show",
        Overview: "",
        ExternalID: "456",
        Provider: "tmdb",
        PosterURL: "/correct-show.jpg",
        BackdropURL: "",
        ReleaseDate: "2024-01-01",
        VoteAverage: 0,
      },
    ]);
    vi.spyOn(api, "identifyShow").mockResolvedValue({ updated: 1 });

    render(
      <IdentifyShowDialog
        open
        onOpenChange={onOpenChange}
        libraryId={1}
        showKey="title-missingshow2024"
        showTitle="Missing Show (2024)"
        onSuccess={onSuccess}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Search/i }));
    fireEvent.click(await screen.findByRole("button", { name: /Choose/i }));

    await waitFor(() => {
      expect(api.identifyShow).toHaveBeenCalledWith(1, "title-missingshow2024", 456);
    });
    expect(queueLibraryIdentify).toHaveBeenCalledWith(1, {
      abortActive: true,
      prioritize: true,
      resetState: true,
    });
    expect(onSuccess).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("does not show TV failure UI after a successful identify while stale rows are still visible", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 1, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    const fetchLibraryMedia = vi.spyOn(api, "fetchLibraryMedia");
    fetchLibraryMedia.mockResolvedValueOnce([
      {
        id: 42,
        title: "Easy Show - S01E01 - Pilot",
        path: "/tv/Easy Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
    ]);
    fetchLibraryMedia.mockResolvedValueOnce([
      {
        id: 42,
        title: "Easy Show - S01E01 - Pilot",
        path: "/tv/Easy Show/S01E01.mkv",
        duration: 1800,
        type: "tv",
        match_status: "local",
        season: 1,
        episode: 1,
      },
    ]);
    vi.mocked(api.identifyLibrary).mockResolvedValue({ identified: 1, failed: 0 });

    renderApp();

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1);
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Identify manually/i })).not.toBeInTheDocument();
  });

  it("shows retry identify for failed movie auto-matches", async () => {
    const retryIdentify = deferred<{ identified: number; failed: number }>();

    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Die My Love",
        path: "/movies/Die My Love (2025)/Die My Love.mp4",
        duration: 7200,
        type: "movie",
        match_status: "unmatched",
      },
    ]);
    vi.mocked(api.identifyLibrary)
      .mockResolvedValueOnce({ identified: 0, failed: 1 })
      .mockImplementationOnce(() => retryIdentify.promise);

    renderApp();

    expect(await screen.findByRole("button", { name: /Retry identify/i })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Retry identify/i }));

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(2);
    });
    expect(identifyLibraryIds()).toEqual([2, 2]);

    await act(async () => {
      retryIdentify.resolve({ identified: 0, failed: 1 });
      await Promise.resolve();
    });
  });

  it("prefers the local identify phase over persisted failed scan status", async () => {
    const activeIdentify = deferred<{ identified: number; failed: number }>();

    vi.spyOn(api, "getLibraryScanStatus").mockResolvedValue({
      libraryId: 2,
      phase: "completed",
      enriching: false,
      identifyPhase: "failed",
      identified: 0,
      identifyFailed: 1,
      processed: 1,
      added: 1,
      updated: 0,
      removed: 0,
      unmatched: 1,
      skipped: 0,
      identifyRequested: false,
      estimatedItems: 1,
      queuePosition: 0,
    });
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Die My Love",
        path: "/movies/Die My Love (2025)/Die My Love.mp4",
        duration: 7200,
        type: "movie",
        match_status: "unmatched",
      },
    ]);
    vi.mocked(api.identifyLibrary).mockImplementationOnce(() => activeIdentify.promise);

    renderApp();

    await waitFor(() => {
      expect(api.identifyLibrary).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(screen.getByText("Searching…")).toBeTruthy();
    });
    expect(screen.queryByRole("button", { name: /Retry identify/i })).not.toBeInTheDocument();

    await act(async () => {
      activeIdentify.resolve({ identified: 0, failed: 1 });
      await Promise.resolve();
    });
  });

  it("does not mark movies as failed when they already have poster art but omit match status", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Has Poster",
        path: "/movies/Has Poster (2025)/Has Poster.mp4",
        duration: 7200,
        type: "movie",
        poster_path: "/poster.jpg",
        release_date: "2025-01-01",
      },
    ]);
    vi.mocked(api.identifyLibrary).mockResolvedValue({ identified: 1, failed: 0 });

    renderApp();

    expect(await screen.findByRole("button", { name: /^Has Poster$/i })).toBeTruthy();
    expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Retry identify/i })).not.toBeInTheDocument();
  });

  it("uses poster art from any matched anime episode before showing failed state", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 3, name: "Anime", type: "anime", path: "/anime", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 41,
        title: "Frieren - S01E01 - Journey",
        path: "/anime/Frieren/S01E01.mkv",
        duration: 1800,
        type: "anime",
        match_status: "identified",
        tmdb_id: 123,
        season: 1,
        episode: 1,
      },
      {
        id: 42,
        title: "Frieren - S01E02 - Magic",
        path: "/anime/Frieren/S01E02.mkv",
        duration: 1800,
        type: "anime",
        match_status: "identified",
        tmdb_id: 123,
        poster_path: "/frieren.jpg",
        season: 1,
        episode: 2,
      },
    ]);
    vi.mocked(api.identifyLibrary).mockResolvedValue({ identified: 2, failed: 0 });

    renderApp();

    expect(await screen.findByRole("link", { name: /Frieren/i })).toBeTruthy();
    expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Identify manually/i })).not.toBeInTheDocument();
  });

  it("keeps incomplete anime cards hidden until they have a review prompt or real match", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 3, name: "Anime", type: "anime", path: "/anime", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 41,
        title: "Dragonball - S01E02 - The Emperor's Quest",
        path: "/anime/Dragonball/S01E02.mkv",
        duration: 1800,
        type: "anime",
        match_status: "unmatched",
        season: 1,
        episode: 2,
      },
    ]);
    vi.mocked(api.identifyLibrary).mockResolvedValue({ identified: 0, failed: 1 });

    renderApp();

    await waitFor(() => {
      expect(screen.queryByRole("link", { name: /Dragonball/i })).not.toBeInTheDocument();
    });
    expect(screen.queryByText("Couldn't match automatically")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Identify manually/i })).not.toBeInTheDocument();
  });

  it("shows a review prompt for anime fallback matches and confirms it", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 3, name: "Anime", type: "anime", path: "/anime", user_id: 1 },
    ]);
    const fetchLibraryMedia = vi.spyOn(api, "fetchLibraryMedia");
    fetchLibraryMedia.mockResolvedValue([
      {
        id: 41,
        title: "Frieren - S01E01 - Journey",
        path: "/anime/Frieren/S01E01.mkv",
        duration: 1800,
        type: "anime",
        match_status: "identified",
        tmdb_id: 123,
        poster_path: "/frieren.jpg",
        metadata_review_needed: false,
        season: 1,
        episode: 1,
      },
    ]);
    fetchLibraryMedia.mockResolvedValueOnce([
      {
        id: 41,
        title: "Frieren - S01E01 - Journey",
        path: "/anime/Frieren/S01E01.mkv",
        duration: 1800,
        type: "anime",
        match_status: "identified",
        tmdb_id: 123,
        poster_path: "/frieren.jpg",
        metadata_review_needed: true,
        season: 1,
        episode: 1,
      },
    ]);
    const identifyRequest = deferred<{ identified: number; failed: number }>();
    vi.mocked(api.identifyLibrary).mockImplementation(() => identifyRequest.promise);

    renderApp();

    expect(await screen.findByText("Is this correct?")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Confirm/i }));

    await waitFor(() => {
      expect(api.confirmShow).toHaveBeenCalledWith(3, { showKey: "tmdb-123" });
    });
    await waitFor(() => {
      expect(screen.queryByText("Is this correct?")).not.toBeInTheDocument();
    });
    await act(async () => {
      identifyRequest.resolve({ identified: 1, failed: 0 });
      await Promise.resolve();
    });
  });

  it("falls back to a placeholder poster when poster loading fails", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Broken Poster",
        path: "/movies/Broken Poster (2025)/Broken Poster.mp4",
        duration: 7200,
        type: "movie",
        match_status: "identified",
        poster_path: "/broken.jpg",
        release_date: "2025-01-01",
      },
    ]);

    renderApp();

    const movieCard = await screen.findByRole("button", { name: /^Broken Poster$/i });
    const poster = movieCard.closest(".show-card")?.querySelector("img") as HTMLImageElement | null;
    expect(poster).toBeTruthy();
    expect(poster?.getAttribute("src")).toContain("/broken.jpg");

    fireEvent.error(poster!);

    await waitFor(() => {
      const fallbackPoster = movieCard.closest(".show-card")?.querySelector("img");
      expect(fallbackPoster).toHaveAttribute("src", "/placeholder-poster.svg");
    });
  });

  it("finishes onboarding after scan-only import without waiting for identify", async () => {
    mockAuthSession({ hasAdmin: false, user: null });
    vi.spyOn(api, "getSetupStatus").mockResolvedValue({ hasAdmin: true });
    vi.spyOn(api, "createAdmin").mockResolvedValue({
      id: 1,
      email: "admin@example.com",
      is_admin: true,
    });
    vi.spyOn(api, "createLibrary").mockResolvedValue({
      id: 10,
      name: "TV",
      type: "tv",
      path: "/tv",
      user_id: 1,
    });
    vi.spyOn(api, "startLibraryScan").mockResolvedValue({
      libraryId: 10,
      phase: "queued",
      enriching: false,
      identifyPhase: "idle",
      identified: 0,
      identifyFailed: 0,
      processed: 0,
      added: 0,
      updated: 0,
      removed: 0,
      unmatched: 0,
      skipped: 0,
      identifyRequested: false,
      estimatedItems: 0,
      queuePosition: 1,
      startedAt: new Date().toISOString(),
    });
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 10, name: "TV", type: "tv", path: "/tv", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([]);
    vi.mocked(api.identifyLibrary).mockImplementation(() => new Promise(() => {}));

    renderApp();

    expect(await screen.findByRole("heading", { name: /Create admin account/i })).toBeTruthy();

    fireEvent.change(screen.getByLabelText(/Email/i), {
      target: { value: "admin@example.com" },
    });
    fireEvent.change(screen.getByLabelText(/^Password/i), {
      target: { value: "passwordpassword" },
    });
    fireEvent.change(screen.getByLabelText(/Confirm password/i), {
      target: { value: "passwordpassword" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Create admin/i }));

    expect(await screen.findByRole("heading", { name: /Add libraries/i })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Add libraries manually instead/i }));

    fireEvent.change(screen.getByLabelText(/Library name/i), {
      target: { value: "TV" },
    });
    fireEvent.change(screen.getByLabelText(/Folder path/i), {
      target: { value: "/tv" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^Add library$/i }));

    await waitFor(() => {
      expect(api.startLibraryScan).toHaveBeenCalledWith(10, { identify: false });
    });

    fireEvent.click(screen.getByRole("button", { name: /Finish setup/i }));

    expect(await screen.findByText(/No media in this library yet/i)).toBeTruthy();
  });

  it("auto-enters the app after adding default libraries with scan-only import", async () => {
    mockAuthSession({ hasAdmin: false, user: null });
    vi.spyOn(api, "getSetupStatus").mockResolvedValue({ hasAdmin: true });
    vi.spyOn(api, "createAdmin").mockResolvedValue({
      id: 1,
      email: "admin@example.com",
      is_admin: true,
    });
    vi.spyOn(api, "createLibrary")
      .mockResolvedValueOnce({ id: 11, name: "TV", type: "tv", path: "/tv", user_id: 1 })
      .mockResolvedValueOnce({ id: 12, name: "Movies", type: "movie", path: "/movies", user_id: 1 })
      .mockResolvedValueOnce({ id: 13, name: "Anime", type: "anime", path: "/anime", user_id: 1 })
      .mockResolvedValueOnce({ id: 14, name: "Music", type: "music", path: "/music", user_id: 1 });
    vi.spyOn(api, "startLibraryScan").mockImplementation(async (libraryId) => ({
      libraryId,
      phase: "queued",
      enriching: false,
      identifyPhase: "idle",
      identified: 0,
      identifyFailed: 0,
      processed: 0,
      added: 0,
      updated: 0,
      removed: 0,
      unmatched: 0,
      skipped: 0,
      identifyRequested: false,
      estimatedItems: 0,
      queuePosition: 1,
      startedAt: new Date().toISOString(),
    }));
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 11, name: "TV", type: "tv", path: "/tv", user_id: 1 },
      { id: 12, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
      { id: 13, name: "Anime", type: "anime", path: "/anime", user_id: 1 },
      { id: 14, name: "Music", type: "music", path: "/music", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([]);

    renderApp();

    expect(await screen.findByRole("heading", { name: /Create admin account/i })).toBeTruthy();

    fireEvent.change(screen.getByLabelText(/Email/i), {
      target: { value: "admin@example.com" },
    });
    fireEvent.change(screen.getByLabelText(/^Password/i), {
      target: { value: "passwordpassword" },
    });
    fireEvent.change(screen.getByLabelText(/Confirm password/i), {
      target: { value: "passwordpassword" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Create admin/i }));

    expect(await screen.findByRole("heading", { name: /Add libraries/i })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Add default libraries and continue/i }));

    await waitFor(() => {
      expect(api.startLibraryScan).toHaveBeenNthCalledWith(1, 11, { identify: false });
      expect(api.startLibraryScan).toHaveBeenNthCalledWith(2, 12, { identify: false });
      expect(api.startLibraryScan).toHaveBeenNthCalledWith(3, 13, { identify: false });
      expect(api.startLibraryScan).toHaveBeenNthCalledWith(4, 14, { identify: false });
    });

    expect(await screen.findByText(/No media in this library yet/i)).toBeTruthy();
  });

  it("renders transcoding settings for admins", async () => {
    window.history.pushState({}, "", "/settings");
    vi.spyOn(api, "listLibraries").mockResolvedValue([]);
    vi.spyOn(api, "getTranscodingSettings").mockResolvedValue({
      settings: defaultTranscodingSettings,
      warnings: [],
    });

    renderApp();

    expect(await screen.findByRole("heading", { name: /Transcoding/i })).toBeTruthy();
    expect(await screen.findByLabelText(/VAAPI device/i)).toHaveValue("/dev/dri/renderD128");
    expect(await screen.findByLabelText(/Enable hardware encoding/i)).not.toBeChecked();
    expect(await screen.findByLabelText(/HEVC 10-bit/i)).toBeChecked();
  });

  it("saves transcoding settings updates", async () => {
    window.history.pushState({}, "", "/settings");
    vi.spyOn(api, "listLibraries").mockResolvedValue([]);
    const baseSettings = defaultTranscodingSettings;

    vi.spyOn(api, "getTranscodingSettings").mockResolvedValue({
      settings: baseSettings,
      warnings: [],
    });
    const updateSpy = vi.spyOn(api, "updateTranscodingSettings").mockResolvedValue({
      settings: {
        ...baseSettings,
        vaapiEnabled: true,
        hardwareEncodingEnabled: true,
        encodeFormats: {
          h264: true,
          hevc: true,
          av1: false,
        },
        preferredHardwareEncodeFormat: "hevc",
      },
      warnings: [],
    });

    renderApp();

    fireEvent.click(await screen.findByLabelText(/Enable VAAPI/i));
    fireEvent.click(screen.getByLabelText(/Enable hardware encoding/i));
    const hevcEncodeCard = screen
      .getAllByText(/^HEVC$/i, { selector: "span" })
      .at(-1)
      ?.closest("label");
    if (!hevcEncodeCard) {
      throw new Error("Missing HEVC encode card");
    }
    const hevcEncodeInput = hevcEncodeCard.querySelector("input");
    if (!(hevcEncodeInput instanceof HTMLInputElement)) {
      throw new Error("Missing HEVC encode checkbox");
    }
    fireEvent.click(hevcEncodeInput);
    fireEvent.change(screen.getByLabelText(/Preferred hardware encode format/i), {
      target: { value: "hevc" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Save settings/i }));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalled();
      expect(updateSpy.mock.calls[0]?.[0]).toEqual({
        ...baseSettings,
        vaapiEnabled: true,
        hardwareEncodingEnabled: true,
        encodeFormats: {
          h264: true,
          hevc: true,
          av1: false,
        },
        preferredHardwareEncodeFormat: "hevc",
      });
    });

    expect(await screen.findByText(/Transcoding settings saved./i)).toBeTruthy();
  });

  it("saves library playback defaults from settings", async () => {
    window.history.pushState({}, "", "/settings");
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      {
        id: 14,
        name: "Anime",
        type: "anime",
        path: "/anime",
        user_id: 1,
        preferred_audio_language: "ja",
        preferred_subtitle_language: "en",
        subtitles_enabled_by_default: true,
      },
    ]);
    vi.spyOn(api, "getTranscodingSettings").mockResolvedValue({
      settings: defaultTranscodingSettings,
      warnings: [],
    });
    const updateSpy = vi.spyOn(api, "updateLibraryPlaybackPreferences").mockResolvedValue({
      id: 14,
      name: "Anime",
      type: "anime",
      path: "/anime",
      user_id: 1,
      preferred_audio_language: "ja",
      preferred_subtitle_language: "en",
      subtitles_enabled_by_default: false,
    });

    renderApp();

    fireEvent.click(await screen.findByLabelText(/Enable subtitles by default/i));
    fireEvent.click(screen.getByRole("button", { name: /Save defaults/i }));

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(14, {
        preferred_audio_language: "ja",
        preferred_subtitle_language: "en",
        subtitles_enabled_by_default: false,
      });
    });

    expect(await screen.findByText(/Playback defaults saved./i)).toBeTruthy();
  });

  it("cancels transcode when dismissing a video player", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 2, name: "Movies", type: "movie", path: "/movies", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 99,
        title: "Die My Love",
        path: "/movies/Die My Love (2025)/Die My Love.mp4",
        duration: 7200,
        type: "movie",
        poster_path: "/poster.jpg",
        release_date: "2025-01-01",
      },
    ]);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: /Play Die My Love/i }));
    expect(await screen.findByLabelText("Fullscreen video player")).toBeTruthy();

    fireEvent.click(screen.getAllByRole("button", { name: /Return to docked player/i })[0]!);
    expect(await screen.findByLabelText("Playback dock")).toBeTruthy();

    // Clear mock calls from the pre-play cancel
    vi.mocked(api.closePlaybackSession).mockClear();

    fireEvent.click(screen.getByRole("button", { name: /Close player/i }));

    expect(api.closePlaybackSession).toHaveBeenCalledTimes(1);
  });

  it("does not cancel transcode when dismissing a music player", async () => {
    vi.spyOn(api, "listLibraries").mockResolvedValue([
      { id: 3, name: "Music", type: "music", path: "/music", user_id: 1 },
    ]);
    vi.spyOn(api, "fetchLibraryMedia").mockResolvedValue([
      {
        id: 11,
        title: "Track One",
        path: "/music/Artist/Album/01 - Track One.flac",
        duration: 245,
        type: "music",
        artist: "Artist",
        album: "Album",
        album_artist: "Artist",
        track_number: 1,
      },
    ]);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: /Track One/i }));
    expect(await screen.findByLabelText("Music player")).toBeTruthy();

    vi.mocked(api.closePlaybackSession).mockClear();

    fireEvent.click(screen.getByRole("button", { name: /Close player/i }));

    expect(api.closePlaybackSession).not.toHaveBeenCalled();
  });
});
