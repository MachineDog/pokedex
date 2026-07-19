package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

// purgeVersionImageCache removes only sprites discovered under
// sprites.versions in cached Pokémon API responses. Cache filenames are
// hashes, so the original API JSON is used to reconstruct the exact image URL
// and therefore the exact cache path; unrelated images are never touched.
func (a *app) purgeVersionImageCache() (removed int, released int64, err error) {
	apiDir := filepath.Join(a.cache.dir, "api")
	entries, err := os.ReadDir(apiDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(apiDir, entry.Name()))
		if readErr != nil {
			continue
		}
		var p pokemon
		if json.Unmarshal(data, &p) != nil || p.ID == 0 || len(p.Sprites.Versions) == 0 {
			continue
		}
		for _, image := range pokemonImages(p) {
			if image.Cached || image.URL == "" {
				continue
			}
			path := a.cache.path("images", image.URL)
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			if removeErr := os.Remove(path); removeErr != nil {
				log.Printf("failed to remove cached game-version image %s: %v", path, removeErr)
				continue
			}
			removed++
			released += info.Size()
		}
	}
	return removed, released, nil
}
