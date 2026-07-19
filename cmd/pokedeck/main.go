package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pokeAPI = "https://pokeapi.co/api/v2"

var errPokeAPINotFound = errors.New("PokéAPI resource not found")

type diskCache struct {
	dir string
	mu  sync.Mutex
	in  map[string]*flight
}

type flight struct {
	done chan struct{}
	data []byte
	err  error
}

func newCache(dir string) *diskCache { return &diskCache{dir: dir, in: make(map[string]*flight)} }

func (c *diskCache) path(bucket, key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, bucket, hex.EncodeToString(h[:])+".cache")
}

func (c *diskCache) read(bucket, key string) ([]byte, time.Time, error) {
	p := c.path(bucket, key)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, time.Time{}, err
	}
	return b, st.ModTime(), nil
}

func (c *diskCache) write(bucket, key string, b []byte) error {
	p := c.path(bucket, key)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	tmp := p + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	// Windows does not replace an existing destination with os.Rename.
	// Removing here allows expired cache entries to be refreshed.
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmp, p)
}

func (c *diskCache) get(ctx context.Context, bucket, key string, ttl time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	stale, modified, staleErr := c.read(bucket, key)
	if staleErr == nil && (ttl <= 0 || time.Since(modified) < ttl) {
		return stale, nil
	}
	flightKey := bucket + ":" + key
	c.mu.Lock()
	if f, ok := c.in[flightKey]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			return f.data, f.err
		}
	}
	f := &flight{done: make(chan struct{})}
	c.in[flightKey] = f
	c.mu.Unlock()
	f.data, f.err = load(ctx)
	if f.err == nil {
		f.err = c.write(bucket, key, f.data)
	}
	if f.err != nil && staleErr == nil {
		f.data, f.err = stale, nil
	}
	c.mu.Lock()
	delete(c.in, flightKey)
	close(f.done)
	c.mu.Unlock()
	return f.data, f.err
}

type app struct {
	cache             *diskCache
	apiDB             *apiDatabase
	apiFallback       bool
	client            *http.Client
	dataTTL, imageTTL time.Duration
	page              *template.Template
	catalogMu         sync.RWMutex
	catalog           []listItem
	searchMu          sync.RWMutex
	searchIndex       []card
	imageJobsHigh     chan string
	imageJobsLow      chan string
	imageJobMu        sync.Mutex
	imageQueued       map[string]int
	pageSize          int
}

func (a *app) api(ctx context.Context, path string, out any) error {
	u := pokeAPI + path
	if a.apiDB != nil {
		if b, err := a.apiDB.read(ctx, u, a.dataTTL); err == nil {
			return json.Unmarshal(b, out)
		}
	}
	if a.apiDB != nil && !a.apiFallback {
		return fmt.Errorf("API resource is not present in the packaged database: %s", path)
	}
	b, err := a.cache.get(ctx, "api", u, a.dataTTL, func(ctx context.Context) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%w: %s", errPokeAPINotFound, resp.Status)
			}
			return nil, fmt.Errorf("PokéAPI returned %s", resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	})
	if err != nil {
		return err
	}
	if a.apiDB != nil {
		resourceType, resourceName := apiResourceIdentity(path)
		if err := a.apiDB.write(ctx, u, resourceType, resourceName, b); err != nil {
			return fmt.Errorf("store API database resource: %w", err)
		}
	}
	return json.Unmarshal(b, out)
}

func apiResourceIdentity(path string) (string, string) {
	clean := strings.Trim(strings.SplitN(path, "?", 2)[0], "/")
	parts := strings.Split(clean, "/")
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func localizedName(s species, languages []string, fallback string) string {
	for _, lang := range languages {
		for _, n := range s.Names {
			if n.Language.Name == lang {
				return n.Name
			}
		}
	}
	return fallback
}

func typeDisplay(key string) displayType {
	icon := ""
	return displayType{Key: key, Name: key, Names: map[string]string{"en": key}, Icon: icon}
}

func (a *app) getCard(ctx context.Context, id string) (card, error) {
	var p pokemon
	if err := a.api(ctx, "/pokemon/"+url.PathEscape(strings.ToLower(id)), &p); err != nil {
		return card{}, err
	}
	var s species
	_ = a.api(ctx, "/pokemon-species/"+strconv.Itoa(p.ID), &s)
	c := makeCard(p, s)
	a.localizeTypes(ctx, c.Types)
	a.updateSearchCard(c)
	return c, nil
}

func makeCard(p pokemon, s species) card {
	names := namesMap(s.Names, "en", p.Name)
	c := card{
		ID:       p.ID,
		Name:     preferredLocalizedName(names, p.Name, "zh-hans", "zh-hant", "zh", "en"),
		Japanese: preferredLocalizedName(names, p.Name, "ja-hrkt", "ja", "en"),
		English:  preferredLocalizedName(names, p.Name, "en"),
		Names:    names,
	}
	for _, t := range p.Types {
		c.Types = append(c.Types, typeDisplay(t.Type.Name))
	}
	img := pokemonImage(p)
	if img != "" {
		c.Image = mediaURL(img)
	}
	return c
}

func (a *app) cachedCard(name string) (card, bool) {
	pokemonURL := pokeAPI + "/pokemon/" + url.PathEscape(strings.ToLower(name))
	pokemonJSON, _, err := a.cache.read("api", pokemonURL)
	if err != nil {
		return card{}, false
	}
	var p pokemon
	if json.Unmarshal(pokemonJSON, &p) != nil {
		return card{}, false
	}
	speciesURL := pokeAPI + "/pokemon-species/" + strconv.Itoa(p.ID)
	speciesJSON, _, err := a.cache.read("api", speciesURL)
	if err != nil {
		return card{}, false
	}
	var s species
	if json.Unmarshal(speciesJSON, &s) != nil {
		return card{}, false
	}
	c := makeCard(p, s)
	a.localizeTypesFromCache(c.Types)
	if src := pokemonImage(p); src != "" {
		if _, _, err := a.cache.read("images", src); err != nil {
			c.Image = "/placeholder.svg"
		}
	}
	return c, true
}

func (a *app) loadCards(ctx context.Context, items []listItem, concurrency int) []card {
	cards := make([]card, len(items))
	valid := make([]bool, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i, item := range items {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c, err := a.getCard(ctx, name)
			if err == nil {
				cards[i], valid[i] = c, true
			}
		}(i, item.Name)
	}
	wg.Wait()
	result := make([]card, 0, len(items))
	for i, c := range cards {
		if valid[i] {
			result = append(result, c)
		}
	}
	return result
}

func (a *app) setCatalog(items []listItem) {
	a.catalogMu.Lock()
	a.catalog = append([]listItem(nil), items...)
	a.catalogMu.Unlock()
}

func (a *app) catalogSlice(offset, size int) ([]listItem, int) {
	a.catalogMu.RLock()
	defer a.catalogMu.RUnlock()
	count := len(a.catalog)
	if offset < 0 || offset >= count {
		return nil, count
	}
	end := min(offset+size, count)
	return append([]listItem(nil), a.catalog[offset:end]...), count
}

func (a *app) cardsFromItems(items []listItem) []card {
	cards := make([]card, 0, len(items))
	for _, item := range items {
		if c, ok := a.cachedCard(item.Name); ok {
			cards = append(cards, c)
			continue
		}
		cards = append(cards, card{
			ID:      resultID(item.URL),
			Name:    item.Name,
			English: item.Name,
			Image:   "/placeholder.svg",
		})
	}
	return cards
}

func (a *app) loadCachedCatalog() bool {
	u := pokeAPI + "/pokemon?limit=100000&offset=0"
	b, _, err := a.cache.read("api", u)
	if err != nil {
		return false
	}
	var lr listResponse
	if json.Unmarshal(b, &lr) != nil || len(lr.Results) == 0 {
		return false
	}
	a.setCatalog(lr.Results)
	return true
}

func (a *app) refreshCatalog() {
	var lr listResponse
	if err := a.api(context.Background(), "/pokemon?limit=100000&offset=0", &lr); err != nil {
		log.Printf("failed to update Pokédex index; continuing with local data: %v", err)
		return
	}
	a.setCatalog(lr.Results)
	a.refreshSearchIndex()
	log.Printf("Pokédex index ready: %d entries", len(lr.Results))
}

func (a *app) prefetchPage(page, size int) {
	if page < 1 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var lr listResponse
	if err := a.api(ctx, fmt.Sprintf("/pokemon?limit=%d&offset=%d", size, (page-1)*size), &lr); err != nil {
		return
	}
	a.loadCards(ctx, lr.Results, 4)
}

func resultID(rawURL string) int {
	parts := strings.Split(strings.Trim(rawURL, "/"), "/")
	if len(parts) == 0 {
		return 0
	}
	id, _ := strconv.Atoi(parts[len(parts)-1])
	return id
}

func (a *app) index(w http.ResponseWriter, r *http.Request) {
	a.renderIndex(w, r, "")
}

func (a *app) renderIndex(w http.ResponseWriter, r *http.Request, initialPokemon string) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	pd := pageData{Query: q, InitialPokemon: initialPokemon}
	if q != "" {
		c, err := a.getCard(r.Context(), q)
		if err != nil {
			pd.Error = "Pokémon not found"
		} else {
			pd.Cards = []card{c}
		}
	} else {
		size := a.pageSize
		pd.PageSize = size
		items, count := a.catalogSlice(0, size)
		if count == 0 {
			pd.Error = "The Pokédex index is being prepared in the background; please refresh shortly"
		} else {
			pd.Cards = a.cardsFromItems(items)
			pd.Total = count
			pd.HasMore = len(items) < count
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.page.Execute(w, pd); err != nil {
		log.Println(err)
	}
}

func (a *app) cardsAPI(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	items, total := a.catalogSlice(offset, a.pageSize)
	cards := a.cardsFromItems(items)
	next := offset + len(items)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(cardBatch{
		Cards:      cards,
		NextOffset: next,
		Total:      total,
		HasMore:    next < total,
	})
}

func (a *app) cardAPI(w http.ResponseWriter, r *http.Request) {
	c, err := a.getCard(r.Context(), r.PathValue("name"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		http.Error(w, `{"error":"card unavailable"}`, http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(c)
}

func placeholder(w http.ResponseWriter, r *http.Request) {
	servePlaceholder(w)
}

func servePlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/svg+xml")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "public, max-age=604800")
	}
	io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 300"><rect width="300" height="300" rx="24" fill="#eef1f5"/><circle cx="150" cy="150" r="62" fill="none" stroke="#c5ccd5" stroke-width="14"/><path d="M88 150h124" stroke="#c5ccd5" stroke-width="14"/><circle cx="150" cy="150" r="20" fill="#eef1f5" stroke="#c5ccd5" stroke-width="12"/></svg>`)
}

func (a *app) cacheImage(ctx context.Context, src string) ([]byte, error) {
	return a.cache.get(ctx, "images", src, a.imageTTL, func(ctx context.Context) ([]byte, error) {
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * time.Second):
				}
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
			resp, err := a.client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			if resp.StatusCode != http.StatusOK {
				lastErr = errors.New(resp.Status)
				resp.Body.Close()
				continue
			}
			b, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
			resp.Body.Close()
			if err == nil {
				return b, nil
			}
			lastErr = err
		}
		return nil, lastErr
	})
}

const (
	imagePriorityLow  = 1
	imagePriorityHigh = 2
)

func (a *app) enqueueImage(src string, priority int) {
	if src == "" {
		return
	}
	if _, _, err := a.cache.read("images", src); err == nil {
		return
	}
	a.imageJobMu.Lock()
	current := a.imageQueued[src]
	if current >= priority {
		a.imageJobMu.Unlock()
		return
	}
	a.imageQueued[src] = priority
	a.imageJobMu.Unlock()
	queue := a.imageJobsLow
	if priority == imagePriorityHigh {
		queue = a.imageJobsHigh
	}
	select {
	case queue <- src:
	default:
		a.imageJobMu.Lock()
		if a.imageQueued[src] == priority {
			delete(a.imageQueued, src)
		}
		a.imageJobMu.Unlock()
	}
}

// enqueueImageBackground may wait for low-priority queue capacity. It is only
// called from background goroutines, so a full image queue never blocks an HTTP
// response and no uncached sprite is silently discarded.
func (a *app) enqueueImageBackground(src string) {
	if src == "" {
		return
	}
	if _, _, err := a.cache.read("images", src); err == nil {
		return
	}
	a.imageJobMu.Lock()
	if a.imageQueued[src] >= imagePriorityLow {
		a.imageJobMu.Unlock()
		return
	}
	a.imageQueued[src] = imagePriorityLow
	a.imageJobMu.Unlock()
	a.imageJobsLow <- src
}

func (a *app) nextImageJob() (string, int) {
	select {
	case src := <-a.imageJobsHigh:
		return src, imagePriorityHigh
	default:
	}
	select {
	case src := <-a.imageJobsHigh:
		return src, imagePriorityHigh
	case src := <-a.imageJobsLow:
		return src, imagePriorityLow
	}
}

func (a *app) startImageWorkers(count int) {
	for range count {
		go func() {
			for {
				src, priority := a.nextImageJob()
				a.imageJobMu.Lock()
				current := a.imageQueued[src]
				a.imageJobMu.Unlock()
				if current != priority {
					continue
				}
				_, err := a.cacheImage(context.Background(), src)
				if err != nil {
					log.Printf("background image cache failed for %s: %v", src, err)
				} else {
					log.Printf("background image cache succeeded for %s", src)
				}
				a.imageJobMu.Lock()
				if a.imageQueued[src] == priority {
					delete(a.imageQueued, src)
				}
				a.imageJobMu.Unlock()
			}
		}()
	}
}

func (a *app) warmImageCache(workers int) {
	ctx := context.Background()
	var lr listResponse
	if err := a.api(ctx, "/pokemon?limit=100000&offset=0", &lr); err != nil {
		log.Printf("image precache: failed to read Pokémon list: %v", err)
		return
	}
	a.setCatalog(lr.Results)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				var p pokemon
				err := a.api(ctx, "/pokemon/"+url.PathEscape(name), &p)
				if err == nil {
					for _, image := range pokemonImages(p) {
						if image.Cached {
							a.enqueueImageBackground(image.URL)
						}
					}
					var s species
					err = a.api(ctx, "/pokemon-species/"+strconv.Itoa(p.ID), &s)
					if err != nil && !errors.Is(err, errPokeAPINotFound) {
						log.Printf("detail data precache failed for %s: %v", name, err)
					} else if err == nil {
						a.updateSearchCard(makeCard(p, s))
						// log.Printf("image precache succeeded %s", src)
					}
				}
			}
		}()
	}
	for _, item := range lr.Results {
		jobs <- item.Name
	}
	close(jobs)
	wg.Wait()
	a.refreshSearchIndex()
}

func (a *app) media(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("src")
	u, err := url.Parse(src)
	if err != nil || u.Scheme != "https" || !(u.Host == "raw.githubusercontent.com" || strings.HasSuffix(u.Host, "githubusercontent.com")) {
		http.Error(w, "invalid image source", 400)
		return
	}
	b, _, err := a.cache.read("images", src)
	if err != nil {
		a.enqueueImage(src, imagePriorityHigh)
		w.Header().Set("X-Image-Pending", "true")
		w.Header().Set("Cache-Control", "no-store")
		servePlaceholder(w)
		return
	}
	ext := filepath.Ext(u.Path)
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(b)
}

const indexHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>PokéDeck</title><style>
*{box-sizing:border-box}body{margin:0;overflow:hidden;background:#f4f6fa;color:#18202b;font:15px system-ui,sans-serif}header{background:#d9363e;color:white;padding:18px 5%}h1{margin:0 0 10px;font-size:28px}form{display:flex;max-width:560px}input{flex:1;border:0;border-radius:9px 0 0 9px;padding:11px;font-size:16px}button{border:0;background:#222c3a;color:white;padding:0 20px;border-radius:0 9px 9px 0}main{max-width:1200px;height:calc(100vh - 112px);margin:12px auto 0;padding:0 64px;display:flex;flex-direction:column}.grid{min-height:0;flex:1;display:grid;grid-template-columns:repeat(auto-fill,minmax(190px,1fr));gap:14px;overflow:hidden;padding:7px}.card{position:relative;min-height:0;overflow:hidden;background:white;border:1px solid transparent;border-radius:14px;padding:10px;box-shadow:0 3px 15px #18202b15;transform:translateY(0);transition:transform .65s cubic-bezier(.22,.75,.2,1),box-shadow .55s ease,border-color .45s ease,background .45s ease}.card img{display:block;width:100%;max-height:calc(100% - 88px);aspect-ratio:1;object-fit:contain;background:#f8fafc;border-radius:10px;transition:transform .6s cubic-bezier(.22,.75,.2,1),filter .55s ease}.card:hover{z-index:2;transform:translateY(-7px);border-color:#e85a61;background:#fffdfd;box-shadow:0 14px 32px #d9363e30,0 0 0 3px #d9363e18}.card:hover img{transform:scale(1.035);filter:saturate(1.1) brightness(1.025)}.num{color:#788394;font-size:12px}.name{font-size:17px;font-weight:700;margin:3px 0 1px}.aliases{color:#788394;font-size:12px;min-height:17px}.jp{color:#505b6c;margin-right:6px}.type{display:inline-flex;align-items:center;gap:4px;color:white;font-size:12px;font-weight:700;padding:3px 7px;border-radius:20px;margin:4px 3px 0 0;text-shadow:0 1px 2px #0005}.type-normal{background:#92999f}.type-fire{background:#e8563f}.type-water{background:#3989d5}.type-electric{background:#e5ad16}.type-grass{background:#54a84f}.type-ice{background:#47b9c5}.type-fighting{background:#bc493d}.type-poison{background:#9756a6}.type-ground{background:#b88445}.type-flying{background:#748ed1}.type-psychic{background:#df5680}.type-bug{background:#82952f}.type-rock{background:#9b8753}.type-ghost{background:#655c91}.type-dragon{background:#5966b7}.type-dark{background:#514b48}.type-steel{background:#678b98}.type-fairy{background:#d778a2}.pager{text-align:center;color:#788394;margin:7px}.page-arrow{position:fixed;top:50%;transform:translateY(-50%);z-index:10;width:54px;height:76px;display:grid;place-items:center;border-radius:16px;background:#fff;color:#d9363e;text-decoration:none;font-size:48px;font-weight:300;box-shadow:0 5px 24px #18202b2b;transition:.15s}.page-arrow:hover{background:#d9363e;color:#fff;transform:translateY(-50%) scale(1.06)}.page-prev{left:12px}.page-next{right:12px}.error{text-align:center;padding:50px;color:#9b2430}@media(prefers-reduced-motion:reduce){.card,.card img{transition:none}.card:hover{transform:translateY(-4px)}}@media(max-width:700px){main{padding:0 45px}.page-arrow{width:40px;height:62px;font-size:38px}.page-prev{left:2px}.page-next{right:2px}}</style></head><body data-auto-size="{{.AutoPageSize}}" data-page-size="{{.PageSize}}">
<style>body{overflow:auto}main{display:block;height:auto;min-height:calc(100vh - 112px);padding-bottom:28px}.grid{overflow:visible;grid-auto-rows:auto}.card-link{display:block;min-width:0;color:inherit;text-decoration:none;border-radius:14px}.card-link:focus-visible{outline:3px solid #d9363e;outline-offset:3px}.card{min-height:285px}.card img{max-height:none}.load-sentinel{min-height:72px;display:flex;align-items:center;justify-content:center;gap:12px;color:#788394}.load-spinner{width:22px;height:22px;border:3px solid #d9dee5;border-top-color:#d9363e;border-radius:50%;animation:spin .8s linear infinite}.load-sentinel.done .load-spinner{display:none}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:700px){body{padding-bottom:env(safe-area-inset-bottom)}header{padding:20px 18px 18px}h1{font-size:28px;line-height:1.2;margin-bottom:14px}form{max-width:none}input{min-width:0;padding:12px;font-size:16px}button{padding:0 18px}main{min-height:calc(100vh - 135px);margin:16px auto 0;padding:0 12px 24px}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;padding:0}.card{min-height:245px;overflow:hidden;padding:10px}.card img{width:100%;aspect-ratio:1;object-fit:contain}.name{font-size:16px}.aliases{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.types{min-height:24px}.load-sentinel{min-height:64px}}@media(max-width:340px){.grid{grid-template-columns:1fr}.card{min-height:320px}}</style>
<header><h1>PokéDeck 宝可梦图鉴</h1><form><input name="q" value="{{.Query}}" placeholder="输入名称或全国图鉴编号"><button>搜索</button></form></header><main>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}<div class="grid">{{range .Cards}}<a class="card-link" href="/pokemon/{{.English}}"><article class="card" data-pokemon="{{.English}}"><img src="{{.Image}}" loading="lazy" alt="{{.Name}}"><div class="num">#{{printf "%04d" .ID}}</div><div class="name">{{.Name}}</div><div class="aliases"><span class="jp" lang="ja">{{.Japanese}}</span><span class="en">{{.English}}</span></div><div class="types">{{range .Types}}<span class="type type-{{.Key}}"><span>{{.Icon}}</span>{{.Name}}</span>{{end}}</div></article></a>{{end}}</div>
{{if .Total}}<div id="load-sentinel" class="load-sentinel{{if not .HasMore}} done{{end}}" data-offset="{{len .Cards}}" data-total="{{.Total}}" data-batch="{{.PageSize}}" data-more="{{.HasMore}}"><span class="load-spinner"></span><span class="load-text">已加载 {{len .Cards}} / {{.Total}}</span></div>{{end}}</main><script>
const escapeHTML=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function refreshImage(img,src,delay=1000){
  if(!src||src==='/placeholder.svg')return;
  fetch(src,{cache:'no-store'}).then(r=>{
    if(r.headers.get('X-Image-Pending')==='true'){
      setTimeout(()=>refreshImage(img,src,Math.min(delay*1.6,8000)),delay);
      return null;
    }
    if(!r.ok)throw 0;
    return r.blob();
  }).then(blob=>{
    if(!blob)return;
    const old=img.dataset.objectUrl;
    const next=URL.createObjectURL(blob);
    img.src=next; img.dataset.objectUrl=next;
    if(old)URL.revokeObjectURL(old);
  }).catch(()=>setTimeout(()=>refreshImage(img,src,Math.min(delay*1.6,8000)),delay));
}
function hydrateCard(el){
  if(el.dataset.hydrated==='true')return;
  el.dataset.hydrated='true';
  const name=el.dataset.pokemon;
  fetch('/api/card/'+encodeURIComponent(name)).then(r=>{if(!r.ok)throw 0;return r.json()}).then(c=>{
    el.querySelector('.name').textContent=c.name;
    el.querySelector('.jp').textContent=c.Japanese||c.japanese||'';
    el.querySelector('.en').textContent=c.English||c.english||name;
    refreshImage(el.querySelector('img'),c.Image||c.image);
    const types=c.types||c.Types||[];
    el.querySelector('.types').innerHTML=types.map(t=>'<span class="type type-'+escapeHTML(t.key)+'"><span>'+escapeHTML(t.icon)+'</span>'+escapeHTML(t.name)+'</span>').join('');
  }).catch(()=>{});
}
document.querySelectorAll('[data-pokemon]').forEach(hydrateCard);
function cardMarkup(c){
  const types=(c.types||[]).map(t=>'<span class="type type-'+escapeHTML(t.key)+'"><span>'+escapeHTML(t.icon)+'</span>'+escapeHTML(t.name)+'</span>').join('');
  return '<a class="card-link" href="/pokemon/'+encodeURIComponent(c.english)+'"><article class="card" data-pokemon="'+escapeHTML(c.english)+'"><img src="'+escapeHTML(c.image||'/placeholder.svg')+'" loading="lazy" alt="'+escapeHTML(c.name||c.english)+'"><div class="num">#'+String(c.id).padStart(4,'0')+'</div><div class="name">'+escapeHTML(c.name||c.english)+'</div><div class="aliases"><span class="jp" lang="ja">'+escapeHTML(c.japanese||'')+'</span><span class="en">'+escapeHTML(c.english)+'</span></div><div class="types">'+types+'</div></article></a>';
}
const sentinel=document.getElementById('load-sentinel');
if(sentinel){
  const grid=document.querySelector('.grid');
  let offset=Number(sentinel.dataset.offset);
  let loading=false;
  let hasMore=sentinel.dataset.more==='true';
  const setStatus=text=>sentinel.querySelector('.load-text').textContent=text;
  const loadMore=async()=>{
    if(loading||!hasMore)return;
    loading=true;
    setStatus('正在加载更多…');
    try{
      const r=await fetch('/api/cards?offset='+offset,{cache:'no-store'});
      if(!r.ok)throw 0;
      const batch=await r.json();
      const holder=document.createElement('div');
      holder.innerHTML=batch.cards.map(cardMarkup).join('');
      const added=[...holder.children];
      added.forEach(el=>grid.appendChild(el));
      added.forEach(el=>hydrateCard(el.matches('[data-pokemon]')?el:el.querySelector('[data-pokemon]')));
      offset=batch.next_offset;
      hasMore=batch.has_more;
      sentinel.dataset.offset=String(offset);
      sentinel.dataset.more=String(hasMore);
      setStatus(hasMore?'已加载 '+offset+' / '+batch.total:'已加载全部 '+batch.total+' 只宝可梦');
      if(!hasMore){sentinel.classList.add('done');observer.disconnect()}
    }catch(e){
      setStatus('加载失败，向下滚动重试');
    }finally{loading=false}
  };
  const observer=new IntersectionObserver(entries=>{if(entries.some(e=>e.isIntersecting))loadMore()},{rootMargin:'600px 0px'});
  if(hasMore)observer.observe(sentinel);else sentinel.classList.add('done');
}
</script></body></html>`
