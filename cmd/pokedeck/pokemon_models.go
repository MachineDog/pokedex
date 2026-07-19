package main

import "encoding/json"

type namedResource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type localizedNameEntry struct {
	Name     string        `json:"name"`
	Language namedResource `json:"language"`
}

type listItem struct{ Name, URL string }

type listResponse struct {
	Count   int        `json:"count"`
	Results []listItem `json:"results"`
}

type pokemon struct {
	ID             int            `json:"id"`
	Name           string         `json:"name"`
	Height         int            `json:"height"`
	Weight         int            `json:"weight"`
	BaseExperience int            `json:"base_experience"`
	Sprites        pokemonSprites `json:"sprites"`
	Abilities      []struct {
		IsHidden bool `json:"is_hidden"`
		Ability  struct {
			Name string `json:"name"`
		} `json:"ability"`
	} `json:"abilities"`
	Moves []pokemonMove `json:"moves"`
	Types []struct {
		Type struct {
			Name string `json:"name"`
		} `json:"type"`
	} `json:"types"`
	Stats []struct {
		Base int `json:"base_stat"`
		Stat struct {
			Name string `json:"name"`
		} `json:"stat"`
	} `json:"stats"`
}

type spriteSet struct {
	FrontDefault     string `json:"front_default"`
	BackDefault      string `json:"back_default"`
	FrontFemale      string `json:"front_female"`
	BackFemale       string `json:"back_female"`
	FrontShiny       string `json:"front_shiny"`
	BackShiny        string `json:"back_shiny"`
	FrontShinyFemale string `json:"front_shiny_female"`
	BackShinyFemale  string `json:"back_shiny_female"`
}

type pokemonSprites struct {
	spriteSet
	Other struct {
		Official spriteSet `json:"official-artwork"`
		Home     spriteSet `json:"home"`
		Dream    spriteSet `json:"dream_world"`
		Showdown spriteSet `json:"showdown"`
	} `json:"other"`
	Versions map[string]json.RawMessage `json:"versions"`
}

type pokemonMove struct {
	Move struct {
		Name string `json:"name"`
	} `json:"move"`
	VersionGroupDetails []struct {
		LevelLearnedAt  int `json:"level_learned_at"`
		MoveLearnMethod struct {
			Name string `json:"name"`
		} `json:"move_learn_method"`
		VersionGroup struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"version_group"`
	} `json:"version_group_details"`
}

type species struct {
	Names             []localizedNameEntry `json:"names"`
	FlavorTextEntries []struct {
		FlavorText string `json:"flavor_text"`
		Language   struct {
			Name string `json:"name"`
		} `json:"language"`
	} `json:"flavor_text_entries"`
	Genera []struct {
		Genus    string `json:"genus"`
		Language struct {
			Name string `json:"name"`
		} `json:"language"`
	} `json:"genera"`
	CaptureRate    int           `json:"capture_rate"`
	BaseHappiness  int           `json:"base_happiness"`
	HatchCounter   int           `json:"hatch_counter"`
	GenderRate     int           `json:"gender_rate"`
	IsBaby         bool          `json:"is_baby"`
	IsLegendary    bool          `json:"is_legendary"`
	IsMythical     bool          `json:"is_mythical"`
	Habitat        namedResource `json:"habitat"`
	Color          namedResource `json:"color"`
	Shape          namedResource `json:"shape"`
	GrowthRate     namedResource `json:"growth_rate"`
	Generation     namedResource `json:"generation"`
	EvolutionChain namedResource `json:"evolution_chain"`
}

type card struct {
	ID       int               `json:"id"`
	Name     string            `json:"name,omitempty"`
	Japanese string            `json:"japanese,omitempty"`
	English  string            `json:"english,omitempty"`
	Image    string            `json:"image,omitempty"`
	Types    []displayType     `json:"types,omitempty"`
	Names    map[string]string `json:"names,omitempty"`
}

type displayType struct {
	Key           string            `json:"key"`
	Name          string            `json:"name"`
	Names         map[string]string `json:"names"`
	Icon          string            `json:"icon"`
	NameIconURL   string            `json:"name_icon_url,omitempty"`
	SymbolIconURL string            `json:"symbol_icon_url,omitempty"`
}

type pageData struct {
	Cards                   []card
	Query                   string
	Page, Pages, Prev, Next int
	PageSize                int
	AutoPageSize            bool
	Total                   int
	HasMore                 bool
	Error                   string
	InitialPokemon          string
}

type cardBatch struct {
	Cards      []card `json:"cards"`
	NextOffset int    `json:"next_offset"`
	Total      int    `json:"total"`
	HasMore    bool   `json:"has_more"`
}

func pokemonImage(p pokemon) string {
	if p.Sprites.Other.Official.FrontDefault != "" {
		return p.Sprites.Other.Official.FrontDefault
	}
	return p.Sprites.FrontDefault
}
