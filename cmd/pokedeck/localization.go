package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
)

type translatedResource struct {
	Name         string                      `json:"name"`
	Names        []localizedNameEntry        `json:"names"`
	Descriptions []localizedDescriptionEntry `json:"descriptions"`
	Official     bool                        `json:"official"`
	ISO639       string                      `json:"iso639"`
	ISO3166      string                      `json:"iso3166"`
	Sprites      map[string]map[string]struct {
		NameIcon   string `json:"name_icon"`
		SymbolIcon string `json:"symbol_icon"`
	} `json:"sprites"`
}

func applyLocalizedType(t *displayType, resource translatedResource) {
	names := translatedResourceNames(resource)
	t.Names = names
	t.Name = preferredLocalizedName(names, t.Key, "zh-hans", "zh-hant", "en")
	if generation := resource.Sprites["generation-ix"]; generation != nil {
		if icons, ok := generation["scarlet-violet"]; ok {
			t.NameIconURL = mediaURL(icons.NameIcon)
			t.SymbolIconURL = mediaURL(icons.SymbolIcon)
		}
	}
}

type localizedDescriptionEntry struct {
	Description string        `json:"description"`
	Language    namedResource `json:"language"`
}

type languageOption struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	English  string `json:"english"`
	Official bool   `json:"official"`
}

type languageBatch struct {
	Languages []languageOption `json:"languages"`
}

func namesMap(entries []localizedNameEntry, fallbackKey, fallback string) map[string]string {
	names := make(map[string]string, len(entries)+1)
	for _, entry := range entries {
		if entry.Language.Name != "" && entry.Name != "" {
			names[entry.Language.Name] = entry.Name
		}
	}
	if fallbackKey != "" && names[fallbackKey] == "" {
		names[fallbackKey] = fallback
	}
	return names
}

func preferredLocalizedName(names map[string]string, fallback string, languages ...string) string {
	for _, language := range languages {
		if value := names[language]; value != "" {
			return value
		}
	}
	return fallback
}

func translatedResourceNames(resource translatedResource) map[string]string {
	names := namesMap(resource.Names, "", "")
	for _, entry := range resource.Descriptions {
		if entry.Language.Name != "" && entry.Description != "" && names[entry.Language.Name] == "" {
			names[entry.Language.Name] = entry.Description
		}
	}
	fallback := strings.ReplaceAll(resource.Name, "-", " ")
	if names["en"] == "" {
		names["en"] = fallback
	}
	return names
}

func (a *app) translatedNames(ctx context.Context, resourcePath string) (map[string]string, error) {
	var resource translatedResource
	if err := a.api(ctx, resourcePath, &resource); err != nil {
		return nil, err
	}
	return translatedResourceNames(resource), nil
}

func (a *app) cachedTranslatedNames(resourcePath string) map[string]string {
	u := pokeAPI + resourcePath
	b, _, err := a.cache.read("api", u)
	if err != nil {
		return nil
	}
	var resource translatedResource
	if json.Unmarshal(b, &resource) != nil {
		return nil
	}
	return translatedResourceNames(resource)
}

func (a *app) localizeTypes(ctx context.Context, types []displayType) {
	var wg sync.WaitGroup
	for i := range types {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var resource translatedResource
			resourcePath := "/type/" + url.PathEscape(types[i].Key)
			err := a.api(ctx, resourcePath, &resource)
			if err == nil && len(resource.Sprites) == 0 && a.client != nil {
				_ = os.Remove(a.cache.path("api", pokeAPI+resourcePath))
				err = a.api(ctx, resourcePath, &resource)
			}
			if err == nil {
				applyLocalizedType(&types[i], resource)
			}
		}(i)
	}
	wg.Wait()
}

func (a *app) localizeTypesFromCache(types []displayType) {
	for i := range types {
		u := pokeAPI + "/type/" + url.PathEscape(types[i].Key)
		b, _, err := a.cache.read("api", u)
		if err == nil {
			var resource translatedResource
			if json.Unmarshal(b, &resource) == nil {
				applyLocalizedType(&types[i], resource)
			}
		}
	}
}

func (a *app) languagesAPI(w http.ResponseWriter, r *http.Request) {
	var list listResponse
	if err := a.api(r.Context(), "/language?limit=100&offset=0", &list); err != nil {
		http.Error(w, `{"error":"languages unavailable"}`, http.StatusBadGateway)
		return
	}
	options := make([]languageOption, len(list.Results))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, item := range list.Results {
		wg.Add(1)
		go func(i int, code string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var language translatedResource
			if a.api(r.Context(), "/language/"+url.PathEscape(code), &language) != nil {
				options[i] = languageOption{Code: code, Label: code, English: code}
				return
			}
			names := namesMap(language.Names, "en", code)
			label := preferredLocalizedName(names, code, code, "en")
			options[i] = languageOption{Code: code, Label: label, English: preferredLocalizedName(names, code, "en"), Official: language.Official}
		}(i, item.Name)
	}
	wg.Wait()
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].Official != options[j].Official {
			return options[i].Official
		}
		return options[i].English < options[j].English
	})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(languageBatch{Languages: options})
}
