package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestZeroTTLKeepsCachedDataForever(t *testing.T) {
	cache := newCache(t.TempDir())
	if err := cache.write("api", "resource", []byte("cached")); err != nil {
		t.Fatal(err)
	}
	loads := 0
	got, err := cache.get(context.Background(), "api", "resource", 0, func(context.Context) ([]byte, error) {
		loads++
		return []byte("fresh"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "cached" || loads != 0 {
		t.Fatalf("got %q with %d upstream loads, want permanent cached data", got, loads)
	}
}

func TestTypeDisplayDoesNotUseEmojiFallback(t *testing.T) {
	fire := typeDisplay("fire")
	if fire.Icon != "" {
		t.Fatalf("fire fallback icon = %q, want none", fire.Icon)
	}
	unknown := typeDisplay("unknown")
	if unknown.Icon != "" || unknown.NameIconURL != "" || unknown.SymbolIconURL != "" {
		t.Fatalf("unexpected unknown type display: %+v", unknown)
	}
}

func TestApplyLocalizedTypeUsesAPISprites(t *testing.T) {
	resource := translatedResource{Name: "fire", Names: []localizedNameEntry{{Name: "Fire", Language: namedResource{Name: "en"}}, {Name: "火", Language: namedResource{Name: "zh-hans"}}}}
	resource.Sprites = map[string]map[string]struct {
		NameIcon   string `json:"name_icon"`
		SymbolIcon string `json:"symbol_icon"`
	}{"generation-ix": {"scarlet-violet": {NameIcon: "https://raw.githubusercontent.com/name.png", SymbolIcon: "https://raw.githubusercontent.com/symbol.png"}}}
	display := typeDisplay("fire")
	applyLocalizedType(&display, resource)
	if display.Names["zh-hans"] != "火" || display.NameIconURL == "" || display.SymbolIconURL == "" {
		t.Fatalf("localized type = %+v", display)
	}
}

func TestFallbackResourceNamesFillMissingTranslations(t *testing.T) {
	names := fillMissingNames(map[string]string{"en": "API English"}, fallbackResourceNames("shape", "upright"))
	if names["zh-hans"] != "直立形" || names["ja-hrkt"] != "直立形" || names["en"] != "API English" {
		t.Fatalf("fallback names = %#v", names)
	}
}

func TestImageRequestPromotesQueuedDownload(t *testing.T) {
	a := &app{
		cache:         newCache(t.TempDir()),
		imageJobsHigh: make(chan string, 4),
		imageJobsLow:  make(chan string, 4),
		imageQueued:   make(map[string]int),
	}
	const src = "https://raw.githubusercontent.com/example/image.png"
	a.enqueueImage(src, imagePriorityLow)
	a.enqueueImage(src, imagePriorityHigh)

	got, priority := a.nextImageJob()
	if got != src || priority != imagePriorityHigh {
		t.Fatalf("got %q priority %d, want promoted high-priority job", got, priority)
	}
	if a.imageQueued[src] != imagePriorityHigh {
		t.Fatalf("queued priority = %d, want %d", a.imageQueued[src], imagePriorityHigh)
	}
}

func TestCardsAPIUsesOffsetWithoutUpstreamRequests(t *testing.T) {
	a := &app{cache: newCache(t.TempDir()), pageSize: 4}
	for i := 1; i <= 10; i++ {
		a.catalog = append(a.catalog, listItem{
			Name: "pokemon-" + string(rune('a'+i-1)),
			URL:  "https://pokeapi.co/api/v2/pokemon/" + strconv.Itoa(i) + "/",
		})
	}
	req := httptest.NewRequest("GET", "/api/cards?offset=4", nil)
	w := httptest.NewRecorder()
	a.cardsAPI(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var batch cardBatch
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Cards) != 4 || batch.NextOffset != 8 || !batch.HasMore || batch.Total != 10 {
		t.Fatalf("unexpected batch: %+v", batch)
	}
}

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := []byte("server:\n  address: ':9090'\ncache:\n  directory: './cache'\n  data_ttl: '24h'\n  image_ttl: '48h'\nprecache:\n  enabled: true\n  delay: '1s'\n  scan_workers: 4\n  image_workers: 2\nui:\n  batch_size: 18\n")
	if err := os.WriteFile(path, yaml, 0644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Address != ":9090" || c.UI.BatchSize != 18 || c.Precache.ScanWorkers != 4 {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestPokemonImagesIncludesAlternateAndVersionSprites(t *testing.T) {
	var p pokemon
	p.Sprites.Other.Official.FrontDefault = "https://raw.githubusercontent.com/PokeAPI/a.png"
	p.Sprites.FrontDefault = "https://raw.githubusercontent.com/PokeAPI/b.png"
	p.Sprites.FrontShiny = "https://raw.githubusercontent.com/PokeAPI/c.png"
	p.Sprites.Versions = map[string]json.RawMessage{
		"generation-i": json.RawMessage(`{"red-blue":{"front_default":"https://raw.githubusercontent.com/PokeAPI/d.png","front_shiny":"https://raw.githubusercontent.com/PokeAPI/c.png"}}`),
	}
	images := pokemonImages(p)
	if len(images) != 4 {
		t.Fatalf("got %d unique images, want 4: %+v", len(images), images)
	}
	cached, direct := 0, 0
	for _, image := range images {
		if image.Image == "" || image.URL == "" {
			t.Fatalf("image variant is incomplete: %+v", image)
		}
		if image.Cached {
			cached++
		} else {
			direct++
			if image.Image != image.URL {
				t.Fatalf("version image must use its original URL: %+v", image)
			}
		}
	}
	if cached != 3 || direct != 1 {
		t.Fatalf("cached=%d direct=%d, want 3 and 1", cached, direct)
	}
}

func TestPurgeVersionImageCacheKeepsRegularImages(t *testing.T) {
	cache := newCache(t.TempDir())
	const regular = "https://raw.githubusercontent.com/PokeAPI/regular.png"
	const version = "https://raw.githubusercontent.com/PokeAPI/version.png"
	pokemonJSON := []byte(`{"id":1,"name":"bulbasaur","sprites":{"front_default":"` + regular + `","versions":{"generation-i":{"red-blue":{"front_default":"` + version + `"}}}}}`)
	if err := cache.write("api", "pokemon-1", pokemonJSON); err != nil {
		t.Fatal(err)
	}
	if err := cache.write("images", regular, []byte("regular")); err != nil {
		t.Fatal(err)
	}
	if err := cache.write("images", version, []byte("version")); err != nil {
		t.Fatal(err)
	}
	a := &app{cache: cache}
	removed, released, err := a.purgeVersionImageCache()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || released != int64(len("version")) {
		t.Fatalf("removed=%d released=%d", removed, released)
	}
	if _, _, err := cache.read("images", regular); err != nil {
		t.Fatalf("regular image was removed: %v", err)
	}
	if _, _, err := cache.read("images", version); !os.IsNotExist(err) {
		t.Fatalf("version image still cached: %v", err)
	}
}

func TestMovesAPIUsesCachedHTTPData(t *testing.T) {
	cache := newCache(t.TempDir())
	pokemonJSON := []byte(`{"id":25,"name":"pikachu","moves":[{"move":{"name":"thunder-shock"},"version_group_details":[{"level_learned_at":1,"move_learn_method":{"name":"level-up"},"version_group":{"name":"scarlet-violet"}}]}]}`)
	moveJSON := []byte(`{"id":84,"name":"thunder-shock","accuracy":100,"power":40,"pp":30,"type":{"name":"electric"},"damage_class":{"name":"special"},"names":[{"name":"电击","language":{"name":"zh-hans"}}]}`)
	typeJSON := []byte(`{"name":"electric","names":[{"name":"电","language":{"name":"zh-hans"}},{"name":"Electric","language":{"name":"en"}}]}`)
	damageJSON := []byte(`{"name":"special","names":[{"name":"特殊","language":{"name":"zh-hans"}},{"name":"Special","language":{"name":"en"}}]}`)
	methodJSON := []byte(`{"name":"level-up","names":[{"name":"升级学会","language":{"name":"zh-hans"}},{"name":"Level up","language":{"name":"en"}}]}`)
	if err := cache.write("api", pokeAPI+"/pokemon/pikachu", pokemonJSON); err != nil {
		t.Fatal(err)
	}
	if err := cache.write("api", pokeAPI+"/move/thunder-shock", moveJSON); err != nil {
		t.Fatal(err)
	}
	for resource, data := range map[string][]byte{
		"/type/electric": typeJSON, "/move-damage-class/special": damageJSON, "/move-learn-method/level-up": methodJSON,
	} {
		if err := cache.write("api", pokeAPI+resource, data); err != nil {
			t.Fatal(err)
		}
	}
	a := &app{cache: cache, dataTTL: 24 * time.Hour}
	req := httptest.NewRequest("GET", "/api/pokemon/pikachu/moves", nil)
	req.SetPathValue("name", "pikachu")
	w := httptest.NewRecorder()
	a.movesAPI(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var batch moveBatch
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Moves) != 1 || batch.Moves[0].Name != "电击" || batch.Moves[0].Power != "40" || batch.Moves[0].LearnMethod != "升级学会" {
		t.Fatalf("unexpected move batch: %+v", batch)
	}
}

func TestLatestMoveLearningIgnoresLateAddedRetroGroups(t *testing.T) {
	var pm pokemonMove
	data := []byte(`{"move":{"name":"growl"},"version_group_details":[{"level_learned_at":3,"move_learn_method":{"name":"level-up"},"version_group":{"name":"scarlet-violet","url":"https://pokeapi.co/api/v2/version-group/25/"}},{"level_learned_at":1,"move_learn_method":{"name":"level-up"},"version_group":{"name":"blue-japan","url":"https://pokeapi.co/api/v2/version-group/29/"}}]}`)
	if err := json.Unmarshal(data, &pm); err != nil {
		t.Fatal(err)
	}
	method, level, version := latestMoveLearning(pm)
	if method != "level-up" || level != 3 || version != "scarlet-violet" {
		t.Fatalf("got %s level %d in %s", method, level, version)
	}
}

func TestLocalizedDetailContainsAllLanguages(t *testing.T) {
	var p pokemon
	var s species
	if err := json.Unmarshal([]byte(`{"id":1,"name":"bulbasaur"}`), &p); err != nil {
		t.Fatal(err)
	}
	speciesJSON := []byte(`{"names":[{"name":"妙蛙种子","language":{"name":"zh-hans"}},{"name":"Bulbasaur","language":{"name":"en"}},{"name":"フシギダネ","language":{"name":"ja-hrkt"}}],"flavor_text_entries":[{"flavor_text":"中文描述","language":{"name":"zh-hans"}},{"flavor_text":"English description","language":{"name":"en"}},{"flavor_text":"日本語の説明","language":{"name":"ja-hrkt"}}],"genera":[{"genus":"种子宝可梦","language":{"name":"zh-hans"}},{"genus":"Seed Pokémon","language":{"name":"en"}},{"genus":"たねポケモン","language":{"name":"ja-hrkt"}}],"gender_rate":1,"generation":{"name":"generation-i"}}`)
	if err := json.Unmarshal(speciesJSON, &s); err != nil {
		t.Fatal(err)
	}
	detail := makePokemonDetail(p, s)
	if detail.Localized["zh-hans"].Name != "妙蛙种子" || detail.Localized["en"].Description != "English description" || detail.Localized["ja-hrkt"].Genus != "たねポケモン" {
		t.Fatalf("unexpected localized detail: %+v", detail.Localized)
	}
}

func TestTranslatedResourceUsesGrowthRateDescriptions(t *testing.T) {
	resource := translatedResource{
		Name: "medium-slow",
		Descriptions: []localizedDescriptionEntry{
			{Description: "parabolique", Language: namedResource{Name: "fr"}},
			{Description: "medium slow", Language: namedResource{Name: "en"}},
		},
	}
	names := translatedResourceNames(resource)
	if names["fr"] != "parabolique" || names["en"] != "medium slow" {
		t.Fatalf("unexpected growth-rate translations: %+v", names)
	}
}

func TestSearchAPIFuzzyMatchesSupportedLanguages(t *testing.T) {
	a := &app{searchIndex: []card{
		{ID: 1, Name: "妙蛙种子", Japanese: "フシギダネ", English: "bulbasaur"},
		{ID: 25, Name: "皮卡丘", Japanese: "ピカチュウ", English: "pikachu"},
	}}
	for _, query := range []string{"妙蛙", "ピカチュ", "pikchu", "#25"} {
		req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(query), nil)
		w := httptest.NewRecorder()
		a.searchAPI(w, req)
		if w.Code != 200 {
			t.Fatalf("query %q status=%d", query, w.Code)
		}
		var result searchBatch
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Results) == 0 {
			t.Fatalf("query %q returned no fuzzy matches", query)
		}
	}
}
