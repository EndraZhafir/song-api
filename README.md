# song-api

The Go backend service and CLI database seeder for **Track Guesser** — a web-based song-guessing game where players identify songs from progressively expanding audio snippets.

> **Status:** Early / Phase 1 Prototype (Single-Player Core Loop). Actively under development.

---

## Overview

`song-api` is responsible for:
1. **Seeding Track Metadata:** A CLI tool (`seeder/seed.go`) that queries the **Deezer Public API** for an artist's top tracks and stores them in an embedded **SQLite** database (`game.db`).
2. **Serving Game Rounds:** An HTTP JSON API (`main.go`) that serves available artists, generates randomized rounds scoped to selected artist discographies, refreshes expiring Deezer preview stream URLs on the fly, and provides the scoped autocomplete song pool.

---

## Tech Stack

* **Language:** Go (`1.27+`)
* **HTTP Server:** Go Standard Library (`net/http`) with CORS middleware
* **Database:** SQLite (`modernc.org/sqlite` — pure Go, CGo-free driver)
* **Audio Metadata & Previews:** Deezer Public API (`https://api.deezer.com`)

---

## Project Structure

```text
song-api/
├── seeder/
│   └── seed.go      # CLI script to fetch artist tracks from Deezer into SQLite
├── main.go          # HTTP API server (/api/health, /api/artists, /api/round)
├── game.db          # Embedded SQLite database (auto-created on seed/startup)
├── go.mod           # Go module definition
└── go.sum           # Go module checksums
```

---

## Getting Started

### 1. Seed Artist Tracks into SQLite

Use the seeder CLI to populate `game.db` with tracks from any artist on Deezer:

```bash
# Seed default artist (Radiohead, 25 tracks)
go run ./seeder/seed.go

# Seed a specific artist and custom track limit
go run ./seeder/seed.go -artist "The Strokes" -limit 25
go run ./seeder/seed.go -artist "Keane" -limit 25
```

### 2. Run the API Server

```bash
go run main.go
```

By default, the server starts on `http://localhost:8080`.

#### Environment Variables
| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the HTTP server |
| `DB_PATH` | `game.db` | Path to the SQLite database file |

---

## API Endpoints

### `GET /api/health`
Simple health check endpoint.
```json
{
  "status": "ok"
}
```

### `GET /api/artists`
Returns a sorted list of all distinct artists currently seeded in `game.db`.
```json
{
  "artists": ["Keane", "Radiohead", "The Strokes"]
}
```

### `GET /api/round?artists=Radiohead,The%20Strokes`
Selects a random target song from the specified artist(s), refreshes its signed Deezer 30-second MP3 preview URL so it never expires mid-game, and returns the scoped title pool for frontend autocomplete.
* **Query Parameters:**
  * `artists` *(optional)* — Comma-separated list of artist names. Defaults to all seeded artists if omitted.

```json
{
  "target": {
    "id": 1521864792,
    "title": "Everything In Its Right Place",
    "artist": "Radiohead",
    "preview_url": "https://cdnt-preview.dzcdn.net/api/1/1/..."
  },
  "pool": [
    {
      "id": 1521864792,
      "title": "Everything In Its Right Place",
      "artist": "Radiohead"
    }
  ],
  "artists": ["Radiohead"]
}
```

---

## Roadmap

* [x] **Phase 1:** Deezer CLI Seeder, SQLite storage, and `GET /api/round` single-player API.
* [ ] **Phase 2:** Real-time multiplayer lobbies (4-character room codes), Go WebSocket hub, synchronized playback timestamps, and live room leaderboards.
* [ ] **Phase 3:** Curated genre/era packs and single-binary production deployment via `embed.FS`.
