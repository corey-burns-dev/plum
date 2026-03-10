# AGENTS.md

## Task Completion Requirements

- Both `bun lint` and `bun typecheck` must pass before considering tasks completed.
- Backend tests should be run using `go test ./...` in `apps/server`.

## Project Snapshot

Plum is a lightweight, experimental media server and player suite inspired by platforms like Plex and Jellyfin.

## Core Priorities

1. Performance first.
2. Reliability first.
3. Media playback consistency across devices.

## Maintainability

Long term maintainability is a core priority. If you add new functionality, first check if there are shared logic that can be extracted to a separate module. Duplicate logic across multiple files is a code smell and should be avoided.

Any logic or utilities that are (or should be) common between the web app and the desktop Electron app should live in `@plum/shared` and be consumed from there by both `apps/web` and `apps/desktop` to keep behavior in sync.

## Package Roles

- `apps/server`: Go backend server. Manages media library, transcoding, and SQLite database.
- `apps/web`: React/Vite UI. Modern media player frontend.
- `apps/desktop`: Electron wrapper for the web app, providing a native desktop experience.
- `packages/contracts`: Shared effect/Schema schemas and TypeScript contracts for API and WebSocket protocol.
- `packages/shared`: Shared runtime utilities consumed by both web and desktop.

## Effects & State

The project aims to use `Effect` for managing side effects and domain logic, ensuring robust error handling and composability.

## Future Plans

- Android TV version as a new package in `apps/android-tv`.
- Enhanced transcoding pipeline.
- Multi-user support.
