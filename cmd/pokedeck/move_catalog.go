package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func moveDescription(m moveResource) map[string]string {
	result := make(map[string]string)
	for _, entry := range m.FlavorTextEntries {
		text := strings.Join(strings.Fields(entry.FlavorText), " ")
		if text != "" {
			result[entry.Language.Name] = text
		}
	}
	for _, entry := range m.EffectEntries {
		if result[entry.Language.Name] == "" {
			result[entry.Language.Name] = strings.Join(strings.Fields(entry.ShortEffect), " ")
		}
	}
	return result
}

func (a *app) catalogMove(ctxPath string, r *http.Request) (moveResource, displayMove, error) {
	var m moveResource
	err := a.api(r.Context(), "/move/"+url.PathEscape(strings.ToLower(ctxPath)), &m)
	if err != nil {
		return m, displayMove{}, err
	}
	damageNames, _ := a.translatedNames(r.Context(), "/move-damage-class/"+url.PathEscape(m.DamageClass.Name))
	display := makeDisplayMove(m, pokemonMove{}, damageNames, nil)
	display.LearnMethod, display.LearnKey = "", ""
	types := []displayType{display.Type}
	a.localizeTypes(r.Context(), types)
	display.Type = types[0]
	return m, display, nil
}

func (a *app) moveCatalogAPI(w http.ResponseWriter, r *http.Request) {
	var list listResponse
	if err := a.api(r.Context(), "/move?limit=100000&offset=0", &list); err != nil {
		http.Error(w, `{"error":"move catalog unavailable"}`, http.StatusBadGateway)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	items := make([]listItem, 0, len(list.Results))
	for _, item := range list.Results {
		if q == "" || strings.Contains(strings.ReplaceAll(strings.ToLower(item.Name), "-", " "), q) {
			items = append(items, item)
		}
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 48 {
		limit = 24
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := min(offset+limit, len(items))
	result := make([]displayMove, end-offset)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, item := range items[offset:end] {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_, display, err := a.catalogMove(name, r)
			if err == nil {
				result[i] = display
				return
			}
			fallback := strings.ReplaceAll(name, "-", " ")
			result[i] = displayMove{Key: name, Name: fallback, English: fallback, Names: map[string]string{"en": fallback}, Type: typeDisplay("normal"), Power: "—", Accuracy: "—"}
		}(i, item.Name)
	}
	wg.Wait()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(moveBatch{Moves: result, NextOffset: end, Total: len(items), HasMore: end < len(items)})
}

func (a *app) moveDetailAPI(w http.ResponseWriter, r *http.Request) {
	m, display, err := a.catalogMove(r.PathValue("name"), r)
	if err != nil {
		http.Error(w, `{"error":"move unavailable"}`, http.StatusBadGateway)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 48 {
		limit = 24
	}
	if offset > len(m.LearnedByPokemon) {
		offset = len(m.LearnedByPokemon)
	}
	end := min(offset+limit, len(m.LearnedByPokemon))
	cards := make([]card, end-offset)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, p := range m.LearnedByPokemon[offset:end] {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c, e := a.getCard(r.Context(), name)
			if e == nil {
				cards[i] = c
			}
		}(i, p.Name)
	}
	wg.Wait()
	cards = compactCards(cards)
	generationNames, _ := a.translatedNames(r.Context(), "/generation/"+url.PathEscape(m.Generation.Name))
	targetNames, _ := a.translatedNames(r.Context(), "/move-target/"+url.PathEscape(m.Target.Name))
	response := moveDetailResponse{Move: display, Descriptions: moveDescription(m), Generation: m.Generation.Name, GenerationNames: generationNames, Target: m.Target.Name, TargetNames: targetNames, Priority: m.Priority, Pokemon: cards, PokemonTotal: len(m.LearnedByPokemon), PokemonNext: end, PokemonHasMore: end < len(m.LearnedByPokemon)}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(response)
}

func compactCards(cards []card) []card {
	result := cards[:0]
	for _, c := range cards {
		if c.ID != 0 {
			result = append(result, c)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
