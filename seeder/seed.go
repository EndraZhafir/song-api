package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// The underscore "_" imports the package purely for its database driver registration side-effect
	_ "modernc.org/sqlite"
)

// ==========================================
// 1. DATA STRUCTURES (JSON -> Go Structs)
// ==========================================

// Struct for the artist search response: /search/artist?q=...
type ArtistSearchResponse struct {
	Data []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

// Struct for the artist's top tracks response: /artist/{id}/top?limit=...
type TracksResponse struct {
	Data  []DeezerTrack `json:"data"`
	Total int           `json:"total"`
}

type DeezerTrack struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Preview string `json:"preview"`
}

func main() {
	// ==========================================
	// 2. CLI FLAGS
	// ==========================================
	artistFlag := flag.String("artist", "Radiohead", "Artist name to seed into database")
	limitFlag := flag.Int("limit", 25, "Maximum number of tracks to fetch")
	flag.Parse()

	fmt.Printf("🔍 Looking up artist '%s' on Deezer...\n", *artistFlag)

	// ==========================================
	// 3. FETCH DATA FROM DEEZER
	// ==========================================
	artistID, canonicalName, err := findArtist(*artistFlag)
	if err != nil {
		log.Fatalf("❌ Error finding artist: %v", err)
	}

	fmt.Printf("🎵 Found '%s' (ID: %d). Fetching top %d tracks...\n", canonicalName, artistID, *limitFlag)

	tracks, err := fetchTopTracks(artistID, *limitFlag)
	if err != nil {
		log.Fatalf("❌ Error fetching tracks: %v", err)
	}

	if len(tracks) == 0 {
		fmt.Println("⚠️ No tracks with audio previews found.")
		return
	}

	// ==========================================
	// 4. DATABASE INITIALIZATION
	// ==========================================
	dbPath := resolveDBPath()
	fmt.Printf("💾 Storing tracks in SQLite database: %s\n", dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("❌ Failed to open database: %v", err)
	}
	defer db.Close() // Automatically close database when main() finishes

	if err := createTable(db); err != nil {
		log.Fatalf("❌ Failed to create table: %v", err)
	}

	// ==========================================
	// 5. INSERT TRACKS
	// ==========================================
	insertedCount, err := saveTracks(db, canonicalName, tracks)
	if err != nil {
		log.Fatalf("❌ Failed to save tracks: %v", err)
	}

	fmt.Printf("✅ Success! Seeded %d tracks for '%s'.\n", insertedCount, canonicalName)
}

// findArtist searches Deezer for the artist name and returns their ID and official name
func findArtist(query string) (int64, string, error) {
	searchURL := fmt.Sprintf("https://api.deezer.com/search/artist?q=%s", url.QueryEscape(query))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	var result ArtistSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", err
	}

	if len(result.Data) == 0 {
		return 0, "", fmt.Errorf("artist '%s' not found", query)
	}

	// Pick the first/top matching artist
	return result.Data[0].ID, result.Data[0].Name, nil
}

// fetchTopTracks gets the top tracks for a specific artist ID
func fetchTopTracks(artistID int64, limit int) ([]DeezerTrack, error) {
	tracksURL := fmt.Sprintf("https://api.deezer.com/artist/%d/top?limit=%d", artistID, limit)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(tracksURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result TracksResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Filter tracks to ensure they have preview audio
	var validTracks []DeezerTrack
	for _, track := range result.Data {
		if strings.TrimSpace(track.Preview) != "" {
			validTracks = append(validTracks, track)
		}
	}

	return validTracks, nil
}

// createTable ensures our SQLite tracks table exists
func createTable(db *sql.DB) error {
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

// saveTracks inserts tracks into SQLite inside a transaction
func saveTracks(db *sql.DB, artistName string, tracks []DeezerTrack) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // Safe: will be ignored if tx.Commit() is reached

	query := `INSERT OR IGNORE INTO tracks (id, title, artist, preview_url) VALUES (?, ?, ?, ?)`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	insertedCount := 0
	for _, track := range tracks {
		res, err := stmt.Exec(track.ID, track.Title, artistName, track.Preview)
		if err != nil {
			return insertedCount, err
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected > 0 {
			insertedCount++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return insertedCount, nil
}

// resolveDBPath locates game.db regardless of execution directory
func resolveDBPath() string {
	if _, err := os.Stat("game.db"); err == nil {
		return "game.db"
	}
	if _, err := os.Stat(filepath.Join("..", "game.db")); err == nil {
		return filepath.Join("..", "game.db")
	}
	return "game.db"
}
