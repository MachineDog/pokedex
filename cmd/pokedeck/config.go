package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type config struct {
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`
	Cache struct {
		Directory   string `yaml:"directory"`
		Database    string `yaml:"database"`
		APIFallback bool   `yaml:"api_fallback"`
		DataTTL     string `yaml:"data_ttl"`
		ImageTTL    string `yaml:"image_ttl"`
	} `yaml:"cache"`
	Precache struct {
		Enabled      bool   `yaml:"enabled"`
		Delay        string `yaml:"delay"`
		ScanWorkers  int    `yaml:"scan_workers"`
		ImageWorkers int    `yaml:"image_workers"`
	} `yaml:"precache"`
	UI struct {
		BatchSize int `yaml:"batch_size"`
	} `yaml:"ui"`
}

func defaultConfig() config {
	var c config
	c.Server.Address = ":8080"
	c.Cache.Directory = "./data/cache"
	c.Cache.Database = "./data/pokedeck.db"
	c.Cache.APIFallback = true
	c.Cache.DataTTL = "0"
	c.Cache.ImageTTL = "0"
	c.Precache.Enabled = true
	c.Precache.Delay = "10s"
	c.Precache.ScanWorkers = 8
	c.Precache.ImageWorkers = 2
	c.UI.BatchSize = 24
	return c
}

func loadConfig(path string) (config, error) {
	c := defaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read configuration %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse configuration %s: %w", path, err)
	}
	if c.Server.Address == "" || c.Cache.Directory == "" || c.Cache.Database == "" {
		return c, fmt.Errorf("server.address, cache.directory and cache.database must not be empty")
	}
	if c.Precache.ScanWorkers < 1 || c.Precache.ScanWorkers > 32 || c.Precache.ImageWorkers < 1 || c.Precache.ImageWorkers > 32 {
		return c, fmt.Errorf("worker counts must be between 1 and 32")
	}
	if c.UI.BatchSize < 4 || c.UI.BatchSize > 100 {
		return c, fmt.Errorf("ui.batch_size must be between 4 and 100")
	}
	if _, err := c.dataTTL(); err != nil {
		return c, err
	}
	if _, err := c.imageTTL(); err != nil {
		return c, err
	}
	if _, err := c.precacheDelay(); err != nil {
		return c, err
	}
	return c, nil
}

func parseConfigDuration(name, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("configuration %s=%q is not a valid duration: %w", name, value, err)
	}
	return d, nil
}

func (c config) dataTTL() (time.Duration, error) {
	return parseConfigDuration("cache.data_ttl", c.Cache.DataTTL)
}
func (c config) imageTTL() (time.Duration, error) {
	return parseConfigDuration("cache.image_ttl", c.Cache.ImageTTL)
}
func (c config) precacheDelay() (time.Duration, error) {
	return parseConfigDuration("precache.delay", c.Precache.Delay)
}
