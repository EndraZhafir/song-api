package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ==========================================
// 1. DATA STRUCTURES
// ==========================================

// Track represents a song row stored in SQLite and sent to the client.
type Track struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	PreviewURL string `json:"preview_url"`
}

// PoolItem represents a lightweight option for the frontend autocomplete combobox.
type PoolItem struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

// RoundResponse is the JSON payload returned by GET /api/round.
type RoundResponse struct {
	Target  Track      `json:"target"`
	Pool    []PoolItem `json:"pool"`
	Artists []string   `json:"artists"`
}

// ArtistsResponse is the JSON payload returned by GET /api/artists.
type ArtistsResponse struct {
	Artists []string `json:"artists"`
}

// Server holds shared dependencies like the SQLite database connection.
type Server struct {
	db     *sql.DB
	client *http.Client
}

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "game.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("❌ Failed to open SQLite database (%s): %v", dbPath, err)
	}
	defer db.Close()

	if err := ensureSchema(db); err != nil {
		log.Fatalf("❌ Failed to initialize database schema: %v", err)
	}

	srv := &Server{
		db:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", srv.handleHealth)
	mux.HandleFunc("GET /api/artists", srv.handleGetArtists)
	mux.HandleFunc("GET /api/round", srv.handleGetRound)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	fmt.Printf("🚀 Track Guesser API running on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatalf("❌ Server error: %v", err)
	}
}

// ==========================================
// 2. HTTP HANDLERS
// ==========================================

// handleHealth returns a simple status check.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetArtists returns all distinct artists available in the SQLite database.
func (s *Server) handleGetArtists(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT DISTINCT artist FROM tracks ORDER BY artist COLLATE NOCASE ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query artists")
		return
	}
	defer rows.Close()

	artists := []string{}
	for rows.Next() {
		var artist string
		if err := rows.Scan(&artist); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan artist")
			return
		}
		artists = append(artists, artist)
	}

	writeJSON(w, http.StatusOK, ArtistsResponse{Artists: artists})
}

// handleGetRound selects a random target track and returns the scoped title pool.
// Query param: ?artists=Radiohead,The%20Strokes (optional; defaults to all seeded artists)
func (s *Server) handleGetRound(w http.ResponseWriter, r *http.Request) {
	requestedArtists := parseArtistsParam(r.URL.Query().Get("artists"))

	tracks, err := s.queryTracksByArtists(requestedArtists)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch tracks from database")
		return
	}

	if len(tracks) == 0 {
		writeError(w, http.StatusNotFound, "no tracks found for the selected artist(s); run the seeder first")
		return
	}

	// Pick a random target track from the filtered pool
	target := tracks[rand.IntN(len(tracks))]

	// Refresh the Deezer preview URL if possible (Deezer signed URLs expire after a few hours)
	if freshURL := s.refreshPreviewURL(target.ID); freshURL != "" {
		target.PreviewURL = freshURL
	}

	// Build the scoped autocomplete pool and active artist list
	pool := make([]PoolItem, 0, len(tracks))
	artistSet := make(map[string]struct{})
	seenTitles := make(map[string]struct{})

	for _, t := range tracks {
		artistSet[t.Artist] = struct{}{}
		key := strings.ToLower(t.Artist + "::" + t.Title)
		if _, exists := seenTitles[key]; !exists {
			seenTitles[key] = struct{}{}
			pool = append(pool, PoolItem{
				ID:     t.ID,
				Title:  t.Title,
				Artist: t.Artist,
			})
		}
	}

	activeArtists := make([]string, 0, len(artistSet))
	for a := range artistSet {
		activeArtists = append(activeArtists, a)
	}

	writeJSON(w, http.StatusOK, RoundResponse{
		Target:  target,
		Pool:    pool,
		Artists: activeArtists,
	})
}

// ==========================================
// 3. DATABASE & DEEZER HELPERS
// ==========================================

func ensureSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY,
		title TEXT NOT NULL,
		artist TEXT NOT NULL,
		preview_url TEXT NOT NULL
	);
	`
	_, err := db.Exec(schema)
	return err
}

func parseArtistsParam(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var artists []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			artists = append(artists, trimmed)
		}
	}
	return artists
}

func (s *Server) queryTracksByArtists(artists []string) ([]Track, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if len(artists) == 0 {
		rows, err = s.db.Query(`SELECT id, title, artist, preview_url FROM tracks ORDER BY title COLLATE NOCASE ASC`)
	} else {
		placeholders := make([]string, len(artists))
		args := make([]any, len(artists))
		for i, a := range artists {
			placeholders[i] = "LOWER(artist) = LOWER(?)"
			args[i] = a
		}
		query := fmt.Sprintf(
			`SELECT id, title, artist, preview_url FROM tracks WHERE %s ORDER BY title COLLATE NOCASE ASC`,
			strings.Join(placeholders, " OR "),
		)
		rows, err = s.db.Query(query, args...)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.PreviewURL); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

// refreshPreviewURL fetches the latest signed preview_url from Deezer for a track ID
// and updates SQLite so cached URLs never expire during gameplay.
func (s *Server) refreshPreviewURL(trackID int64) string {
	url := fmt.Sprintf("https://api.deezer.com/track/%d", trackID)
	resp, err := s.client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	var data struct {
		Preview string `json:"preview"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || strings.TrimSpace(data.Preview) == "" {
		return ""
	}

	_, _ = s.db.Exec(`UPDATE tracks SET preview_url = ? WHERE id = ?`, data.Preview, trackID)
	return data.Preview
}

// ==========================================
// 4. MIDDLEWARE & RESPONSE HELPERS
// ==========================================

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
