import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

const originalConsoleLog = console.log;

Object.defineProperties(HTMLMediaElement.prototype, {
  pause: {
    configurable: true,
    value: vi.fn(),
  },
  play: {
    configurable: true,
    value: vi.fn().mockResolvedValue(undefined),
  },
});

vi.spyOn(console, "log").mockImplementation((...args: unknown[]) => {
  if (typeof args[0] === "string" && args[0].startsWith("[api]")) {
    return;
  }

  originalConsoleLog(...args);
});

// Mock WebSocket to avoid undici/jsdom Event instance issues
class MockWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;

  private listeners: Record<string, Set<(event: Event) => void>> = {
    open: new Set(),
    message: new Set(),
    error: new Set(),
    close: new Set(),
  };

  readyState: number = MockWebSocket.CONNECTING;
  url: string;

  constructor(url: string) {
    this.url = url;
    setTimeout(() => {
      this.readyState = MockWebSocket.OPEN;
      this.emit("open", new Event("open"));
    }, 0);
  }

  send(_data: string | ArrayBufferLike | Blob | ArrayBufferView) {}
  close(code?: number, reason?: string) {
    this.readyState = MockWebSocket.CLOSED;
    this.emit(
      "close",
      {
        code: code ?? 1000,
        reason: reason ?? "",
        wasClean: true,
      } as CloseEvent,
    );
  }

  private emit(type: string, event: Event) {
    if (type === "open") {
      this.onopen?.(event);
    } else if (type === "message") {
      this.onmessage?.(event as MessageEvent);
    } else if (type === "error") {
      this.onerror?.(event);
    } else if (type === "close") {
      this.onclose?.(event as CloseEvent);
    }

    const listeners = this.listeners[type];
    if (!listeners) return;
    for (const listener of listeners) {
      listener(event);
    }
  }

  addEventListener(type: string, listener: EventListener) {
    const listeners = this.listeners[type];
    if (!listeners) return;
    listeners.add(listener as (event: Event) => void);
  }

  removeEventListener(type: string, listener: EventListener) {
    const listeners = this.listeners[type];
    if (!listeners) return;
    listeners.delete(listener as (event: Event) => void);
  }

  dispatchEvent() {
    return true;
  }
}

vi.stubGlobal("WebSocket", MockWebSocket);
