package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"time"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	initDatabase := flag.String("init-database", "", "initialize an empty PokéAPI SQLite database and exit")
	checkDatabase := flag.String("check-database", "", "validate a PokéAPI SQLite database and print its record count")
	purgeVersionImages := flag.Bool("purge-version-images", false, "delete cached game-version images and exit")
	flag.Parse()
	if *initDatabase != "" {
		database, err := openAPIDatabase(*initDatabase)
		if err != nil {
			log.Fatal(err)
		}
		database.close()
		return
	}
	if *checkDatabase != "" {
		database, err := openAPIDatabase(*checkDatabase)
		if err != nil {
			log.Fatal(err)
		}
		defer database.close()
		count, err := database.count()
		if err != nil {
			log.Fatal(err)
		}
		if count == 0 {
			log.Fatal("database contains no API resources")
		}
		log.Printf("database contains %d API resources", count)
		return
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	dataTTL, _ := cfg.dataTTL()
	imageTTL, _ := cfg.imageTTL()
	precacheDelay, _ := cfg.precacheDelay()
	t := template.Must(template.New("index").Parse(indexHTMLV2))
	apiDB, err := openAPIDatabase(cfg.Cache.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer apiDB.close()
	a := &app{cache: newCache(cfg.Cache.Directory), apiDB: apiDB, apiFallback: cfg.Cache.APIFallback, client: &http.Client{Timeout: 60 * time.Second}, dataTTL: dataTTL, imageTTL: imageTTL, page: t, imageJobsHigh: make(chan string, 4096), imageJobsLow: make(chan string, 4096), imageQueued: make(map[string]int), pageSize: cfg.UI.BatchSize}
	if a.loadCachedCatalog() {
		log.Printf("loaded Pokédex index from local cache")
		go a.refreshSearchIndex()
	}
	if removed, released, err := a.purgeVersionImageCache(); err != nil {
		log.Printf("failed to purge game-version image cache: %v", err)
	} else if removed > 0 {
		log.Printf("deleted %d cached game-version images and released %.1f MiB", removed, float64(released)/(1024*1024))
	}
	if *purgeVersionImages {
		return
	}
	a.startImageWorkers(cfg.Precache.ImageWorkers)
	go a.refreshCatalog()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.index)
	mux.HandleFunc("GET /media/pokemon/{id}", a.media)
	mux.HandleFunc("GET /media/image", a.media)
	mux.HandleFunc("GET /api/card/{name}", a.cardAPI)
	mux.HandleFunc("GET /api/cards", a.cardsAPI)
	mux.HandleFunc("GET /api/search", a.searchAPI)
	mux.HandleFunc("GET /api/languages", a.languagesAPI)
	mux.HandleFunc("GET /api/pokemon/{name}", a.pokemonDetailAPI)
	mux.HandleFunc("GET /api/pokemon/{name}/moves", a.movesAPI)
	mux.HandleFunc("GET /api/moves", a.moveCatalogAPI)
	mux.HandleFunc("GET /api/move/{name}", a.moveDetailAPI)
	mux.HandleFunc("GET /pokemon/{name}", a.pokemonDetailPage)
	mux.HandleFunc("GET /moves", a.index)
	mux.HandleFunc("GET /placeholder.svg", placeholder)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	if cfg.Precache.Enabled {
		go func() {
			time.Sleep(precacheDelay)
			a.warmImageCache(cfg.Precache.ScanWorkers)
		}()
	}
	addr := cfg.Server.Address
	log.Printf("Pokédex running at http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
