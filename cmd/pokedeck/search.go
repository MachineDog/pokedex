package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type searchBatch struct {
	Results []card `json:"results"`
}

func normalizeSearch(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '・' || r == '·' {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}

func levenshteinRunes(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ar := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, br := range b {
			cost := 0
			if ar != br {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func fuzzyScore(query, candidate string) (int, bool) {
	q, c := normalizeSearch(query), normalizeSearch(candidate)
	if q == "" || c == "" {
		return 0, false
	}
	if q == c {
		return 0, true
	}
	if strings.HasPrefix(c, q) {
		return 10, true
	}
	if index := strings.Index(c, q); index >= 0 {
		return 25 + index, true
	}
	qr, cr := []rune(q), []rune(c)
	distance := levenshteinRunes(qr, cr)
	limit := max(1, min(3, len(qr)/3+1))
	if distance <= limit {
		return 50 + distance*5 + abs(len(qr)-len(cr)), true
	}
	return 0, false
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (a *app) refreshSearchIndex() {
	a.catalogMu.RLock()
	items := append([]listItem(nil), a.catalog...)
	a.catalogMu.RUnlock()
	if len(items) == 0 {
		return
	}
	cards := a.cardsFromItems(items)
	a.searchMu.Lock()
	a.searchIndex = cards
	a.searchMu.Unlock()
}

func (a *app) updateSearchCard(updated card) {
	if updated.ID <= 0 {
		return
	}
	a.searchMu.Lock()
	defer a.searchMu.Unlock()
	for i := range a.searchIndex {
		if a.searchIndex[i].ID == updated.ID {
			a.searchIndex[i] = updated
			return
		}
	}
	a.searchIndex = append(a.searchIndex, updated)
}

func (a *app) currentSearchIndex() []card {
	a.searchMu.RLock()
	cards := append([]card(nil), a.searchIndex...)
	a.searchMu.RUnlock()
	if len(cards) > 0 {
		return cards
	}
	a.refreshSearchIndex()
	a.searchMu.RLock()
	cards = append([]card(nil), a.searchIndex...)
	a.searchMu.RUnlock()
	return cards
}

func (a *app) searchAPI(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 50 {
		limit = 12
	}
	type match struct {
		card  card
		score int
	}
	matches := make([]match, 0, limit)
	for _, candidate := range a.currentSearchIndex() {
		best, found := 0, false
		values := []string{candidate.Name, candidate.Japanese, candidate.English, strconv.Itoa(candidate.ID), fmtNationalID(candidate.ID)}
		for _, localized := range candidate.Names {
			values = append(values, localized)
		}
		for _, value := range values {
			if score, ok := fuzzyScore(query, value); ok && (!found || score < best) {
				best, found = score, true
			}
		}
		if found {
			matches = append(matches, match{card: candidate, score: best})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return matches[i].card.ID < matches[j].card.ID
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	results := make([]card, len(matches))
	for i, result := range matches {
		results[i] = result.card
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(searchBatch{Results: results})
}

func fmtNationalID(id int) string {
	if id <= 0 {
		return ""
	}
	return "#" + strconv.Itoa(id)
}
