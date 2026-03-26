import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
  close: (code?: number, reason?: string) => void;
  mockMessage: (data: string) => void;
  sentMessages: string[];
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

async function flushMicrotasks() {
  await Promise.resolve();
  await Promise.resolve();
}

function PlayerHarness() {
  const { activeItem, dismissDock, lastEvent, playMovie, videoSourceUrl } =
    usePlayer();

  return (
    <div>
      <button type="button" onClick={() => playMovie(movie)}>
        Play
      </button>
      <button type="button" onClick={() => dismissDock()}>
        Dismiss
      </button>
      <div data-testid="active-media-id">{activeItem?.id ?? ""}</div>
      <div data-testid="last-event">{lastEvent}</div>
      <div data-testid="video-source-url">{videoSourceUrl}</div>
    </div>
  );
}

describe("PlayerContext playback session updates", () => {
  beforeEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.spyOn(api, "listLibraries").mockResolvedValue([]);
    vi.spyOn(api, "createPlaybackSession").mockResolvedValue({
      sessionId: "session-99",
      delivery: "transcode",
      mediaId: 99,
      revision: 1,
      audioIndex: -1,
      status: "starting",
      streamUrl: "/api/playback/sessions/session-99/revisions/1/index.m3u8",
    });
    vi.spyOn(api, "closePlaybackSession").mockResolvedValue();
    vi.spyOn(api, "updatePlaybackSessionAudio").mockResolvedValue({
      sessionId: "session-99",
      delivery: "transcode",
      mediaId: 99,
      revision: 2,
      audioIndex: 1,
      status: "starting",
      streamUrl: "/api/playback/sessions/session-99/revisions/2/index.m3u8",
    });
    (globalThis.WebSocket as unknown as MockWebSocketClass).reset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("ignores unrelated playback events and applies the active session revision", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
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
      expect(api.createPlaybackSession).toHaveBeenCalledWith(
        99,
        expect.objectContaining({
          audioIndex: -1,
          clientCapabilities: expect.any(Object),
        }),
      );
      expect(screen.getByTestId("active-media-id")).toHaveTextContent("99");
      expect(screen.getByTestId("last-event")).toHaveTextContent(
        "Preparing stream...",
      );
      expect(socket.sentMessages).toContain(
        JSON.stringify({
          action: "attach_playback_session",
          sessionId: "session-99",
        }),
      );
    });

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "playback_session_update",
          sessionId: "session-22",
          delivery: "transcode",
          mediaId: 22,
          revision: 1,
          audioIndex: -1,
          status: "ready",
          streamUrl:
            "/api/playback/sessions/session-22/revisions/1/index.m3u8",
        }),
      );
    });

    expect(screen.getByTestId("video-source-url")).toHaveTextContent("");

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "playback_session_update",
          sessionId: "session-99",
          delivery: "transcode",
          mediaId: 99,
          revision: 1,
          audioIndex: -1,
          status: "ready",
          streamUrl:
            "/api/playback/sessions/session-99/revisions/1/index.m3u8",
        }),
      );
    });

    await waitFor(() => {
      expect(screen.getByTestId("last-event")).toHaveTextContent(
        "Stream ready",
      );
      expect(screen.getByTestId("video-source-url")).toHaveTextContent(
        "/api/playback/sessions/session-99/revisions/1/index.m3u8",
      );
    });
  });

  it("reattaches the active playback session after websocket reconnect", async () => {
    vi.useFakeTimers();

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
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
    await act(async () => {
      await vi.runOnlyPendingTimersAsync();
      await vi.runOnlyPendingTimersAsync();
    });
    expect(MockWebSocket.instances.length).toBeGreaterThan(0);

    const firstSocket = MockWebSocket.instances[0];
    if (!firstSocket) {
      throw new Error("Expected a mock WebSocket instance");
    }

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Play" }));
      await flushMicrotasks();
    });
    expect(firstSocket.sentMessages).toContain(
      JSON.stringify({
        action: "attach_playback_session",
        sessionId: "session-99",
      }),
    );

    act(() => {
      firstSocket.close();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
      await vi.runOnlyPendingTimersAsync();
      await vi.runOnlyPendingTimersAsync();
      await flushMicrotasks();
    });

    expect(MockWebSocket.instances.length).toBeGreaterThan(1);

    const secondSocket = MockWebSocket.instances[1];
    if (!secondSocket) {
      throw new Error("Expected a reconnected mock WebSocket instance");
    }

    expect(secondSocket.sentMessages).toContain(
      JSON.stringify({
        action: "attach_playback_session",
        sessionId: "session-99",
      }),
    );
  });

  it("detaches the playback session before closing it from the player", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
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
      expect(api.createPlaybackSession).toHaveBeenCalled();
    });

    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    await waitFor(() => {
      expect(socket.sentMessages).toContain(
        JSON.stringify({
          action: "detach_playback_session",
          sessionId: "session-99",
        }),
      );
      expect(api.closePlaybackSession).toHaveBeenCalledWith("session-99");
    });
  });
});
