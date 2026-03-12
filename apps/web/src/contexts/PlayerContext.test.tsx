import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    BASE_URL: "http://localhost:3000",
  };
});
import type { MediaItem } from "../api";
import * as api from "../api";
import { PlayerProvider, usePlayer } from "./PlayerContext";
import { WsProvider } from "./WsContext";

type MockWebSocketHandle = {
  mockMessage: (data: string) => void;
};

type MockWebSocketClass = {
  instances: MockWebSocketHandle[];
  reset: () => void;
};

const movie: MediaItem = {
  id: 99,
  title: "Die My Love",
  path: "/movies/Die My Love (2025)/Die My Love.mp4",
  duration: 7200,
  type: "movie",
};

function PlayerHarness() {
  const { activeItem, lastEvent, playMovie, videoSourceUrl } = usePlayer();

  return (
    <div>
      <button type="button" onClick={() => playMovie(movie)}>
        Play
      </button>
      <div data-testid="active-media-id">{activeItem?.id ?? ""}</div>
      <div data-testid="last-event">{lastEvent}</div>
      <div data-testid="video-source-url">{videoSourceUrl}</div>
    </div>
  );
}

describe("PlayerContext playback session updates", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, "listLibraries").mockResolvedValue([]);
    vi.spyOn(api, "createPlaybackSession").mockResolvedValue({
      sessionId: "session-99",
      mediaId: 99,
      revision: 1,
      audioIndex: -1,
      status: "starting",
      playlistPath: "/api/playback/sessions/session-99/revisions/1/index.m3u8",
    });
    vi.spyOn(api, "closePlaybackSession").mockResolvedValue();
    vi.spyOn(api, "updatePlaybackSessionAudio").mockResolvedValue({
      sessionId: "session-99",
      mediaId: 99,
      revision: 2,
      audioIndex: 1,
      status: "starting",
      playlistPath: "/api/playback/sessions/session-99/revisions/2/index.m3u8",
    });
    (globalThis.WebSocket as unknown as MockWebSocketClass).reset();
  });

  it("ignores unrelated playback events and applies the active session revision", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <WsProvider>
          <PlayerProvider>
            <PlayerHarness />
          </PlayerProvider>
        </WsProvider>
      </QueryClientProvider>,
    );

    const MockWebSocket = globalThis.WebSocket as unknown as MockWebSocketClass;
    await waitFor(() => {
      expect(MockWebSocket.instances.length).toBeGreaterThan(0);
    });
    const socket = MockWebSocket.instances[0];
    if (!socket) {
      throw new Error("Expected a mock WebSocket instance");
    }

    fireEvent.click(screen.getByRole("button", { name: "Play" }));

    await waitFor(() => {
      expect(api.createPlaybackSession).toHaveBeenCalledWith(99, { audioIndex: -1 });
      expect(screen.getByTestId("active-media-id")).toHaveTextContent("99");
      expect(screen.getByTestId("last-event")).toHaveTextContent("Preparing stream...");
    });

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "playback_session_update",
          sessionId: "session-22",
          mediaId: 22,
          revision: 1,
          audioIndex: -1,
          status: "ready",
          playlistPath: "/api/playback/sessions/session-22/revisions/1/index.m3u8",
        }),
      );
    });

    expect(screen.getByTestId("video-source-url")).toHaveTextContent("");

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "playback_session_update",
          sessionId: "session-99",
          mediaId: 99,
          revision: 1,
          audioIndex: -1,
          status: "ready",
          playlistPath: "/api/playback/sessions/session-99/revisions/1/index.m3u8",
        }),
      );
    });

    await waitFor(() => {
      expect(screen.getByTestId("last-event")).toHaveTextContent("Stream ready");
      expect(screen.getByTestId("video-source-url")).toHaveTextContent(
        "/api/playback/sessions/session-99/revisions/1/index.m3u8",
      );
    });
  });
});
