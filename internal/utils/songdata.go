package utils

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arcaeabot/internal/database"
)

type Song struct {
	ID             string            `json:"id"`
	TitleLocalized map[string]string `json:"title_localized"`
	Artist         string            `json:"artist"`
	BPM            string            `json:"bpm"`
	BPMBase        any               `json:"bpm_base"`
	Version        string            `json:"version"`
	Side           int               `json:"side"`
	Difficulties   []SongDifficulty  `json:"difficulties"`
}

type SongDifficulty struct {
	RatingClass   int    `json:"ratingClass"`
	Rating        int    `json:"rating"`
	RatingPlus    bool   `json:"ratingPlus"`
	ChartDesigner string `json:"chartDesigner"`
}

type songListFile struct {
	Songs []Song `json:"songs"`
}

type AliasEntry struct {
	Alias  string `json:"alias"`
	Source string `json:"source"`
}

func LoadSongs(path string) ([]Song, map[string]Song) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, map[string]Song{}
	}
	var file songListFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, map[string]Song{}
	}
	byID := make(map[string]Song, len(file.Songs))
	for _, song := range file.Songs {
		if song.ID != "" {
			byID[song.ID] = song
		}
	}
	return file.Songs, byID
}

func SongTitle(song Song) string {
	if title := song.TitleLocalized["en"]; title != "" {
		return title
	}
	keys := make([]string, 0, len(song.TitleLocalized))
	for k := range song.TitleLocalized {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if song.TitleLocalized[k] != "" {
			return song.TitleLocalized[k]
		}
	}
	return song.ID
}

func FindJacket(bundlePath, songID string) string {
	for _, dir := range []string{filepath.Join(bundlePath, "dl_"+songID), filepath.Join(bundlePath, songID)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.ToLower(entry.Name())
			if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".gif") {
				return filepath.Join(dir, entry.Name())
			}
		}
	}
	return ""
}

func FindSongsByAlias(ctx context.Context, db *database.Store, songs []Song, byID map[string]Song, query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	lower := strings.ToLower(query)
	if _, ok := byID[query]; ok {
		return []string{query}
	}
	found := map[string]bool{}
	var ids []string
	if kv, err := db.KV("alias_data"); err == nil {
		all, _ := kv.All(ctx)
		for songID, raw := range all {
			var aliases []AliasEntry
			if json.Unmarshal(raw, &aliases) != nil {
				continue
			}
			for _, item := range aliases {
				alias := strings.ToLower(item.Alias)
				if strings.Contains(alias, lower) || strings.Contains(lower, alias) {
					if !found[songID] {
						found[songID] = true
						ids = append(ids, songID)
					}
				}
			}
		}
	}
	for _, song := range songs {
		if strings.Contains(strings.ToLower(song.ID), lower) || strings.Contains(strings.ToLower(SongTitle(song)), lower) || strings.Contains(strings.ToLower(song.Artist), lower) {
			if !found[song.ID] {
				found[song.ID] = true
				ids = append(ids, song.ID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func DifficultyName(diff int) string {
	switch diff {
	case 0:
		return "PST"
	case 1:
		return "PRS"
	case 2:
		return "FTR"
	case 3:
		return "BYD"
	case 4:
		return "ETR"
	default:
		return "UNK"
	}
}
