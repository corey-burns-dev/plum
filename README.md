# Plum 🫐

Plum is a lightweight, experimental media server and player suite inspired by platforms like Plex and Jellyfin. It features a high-performance Go backend for media management and transcoding, paired with a modern React frontend for a seamless viewing experience.

## ✨ Features

- **Media Library:** Browse your media collection with a clean, responsive UI.
- **Real-time Transcoding:** Initiate and monitor transcoding tasks directly from the player.
- **Live Updates:** WebSocket integration provides instant feedback on server-side tasks.
- **Lightweight & Portable:** Uses SQLite for zero-configuration database management.
- **Cross-Platform:** Built with Go and React to run anywhere.

## 🛠 Tech Stack

- **Backend:** [Go](https://go.dev/) (Golang)
- **Frontend:** [React](https://react.dev/), [TypeScript](https://www.typescriptlang.org/), [Vite](https://vitejs.dev/)
- **Database:** [SQLite](https://sqlite.org/) (via `modernc.org/sqlite`)
- **Communication:** RESTful API & WebSockets

## 🚀 Getting Started

### Prerequisites

- [Go](https://go.dev/doc/install) (1.21 or later)
- [Node.js](https://nodejs.org/) (v20 or later)
- [npm](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm)

### 1. Backend Setup

```bash
cd backend
go run cmd/plum/main.go
```
The backend will start at `http://localhost:8080` by default and create a `plum.db` file in the current directory.

### 2. Frontend Setup

```bash
cd frontend
npm install
npm run dev
```
The frontend will be available at `http://localhost:5173`.

## ⚙️ Configuration

### Backend Environment Variables
- `PLUM_ADDR`: The address and port to listen on (default: `:8080`).
- `PLUM_DB_PATH`: The file path for the SQLite database (default: `./plum.db`).

### Frontend Environment Variables
- `VITE_WS_URL`: The WebSocket URL for the backend (defaults to `ws://localhost:8080/ws` in development).

## 🗺 Roadmap

- [ ] Automatic library scanning
- [ ] Multiple library support (Movies, TV Shows, Music)
- [ ] User authentication and profiles
- [ ] Advanced transcoding options (bitrate, resolution)
- [ ] Mobile-optimized player

## 📄 License

MIT
