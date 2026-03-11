import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
  const { activeItem, lastEvent, playMovie, transcodeVersion } = usePlayer();

  return (
    <div>
      <button type="button" onClick={() => playMovie(movie)}>
        Play
      </button>
      <div data-testid="active-media-id">{activeItem?.id ?? ""}</div>
      <div data-testid="last-event">{lastEvent}</div>
      <div data-testid="transcode-version">{transcodeVersion}</div>
    </div>
  );
}

describe("PlayerContext WebSocket transcode events", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
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

  it("ignores transcode events for non-active media and applies them for the active item", async () => {
    render(
      <PlayerProvider>
        <PlayerHarness />
      </PlayerProvider>,
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
    });

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "transcode_started",
          id: 42,
          preferredMode: "software",
        }),
      );
    });
    expect(screen.getByTestId("last-event")).not.toHaveTextContent("Transcoding...");
    expect(screen.getByTestId("transcode-version")).toHaveTextContent("0");

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "transcode_complete",
          id: 42,
          output: "/tmp/plum_transcoded_42.mp4",
          elapsed: 1.2,
          mode: "software",
          fallbackUsed: false,
          success: true,
          error: "",
        }),
      );
    });
    expect(screen.getByTestId("transcode-version")).toHaveTextContent("0");

    act(() => {
      socket.mockMessage(
        JSON.stringify({
          type: "playback_session_update",
          sessionId: "session-99",
          mediaId: 99,
          revision: 1,
          audioIndex: -1,
          status: "starting",
          playlistPath: "/api/playback/sessions/session-99/revisions/1/index.m3u8",
        }),
      );
    });

    await waitFor(() => {
      expect(screen.getByTestId("last-event")).toHaveTextContent("Preparing stream...");
    });

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
      expect(screen.getByTestId("transcode-version")).toHaveTextContent("1");
    });
  });
});
