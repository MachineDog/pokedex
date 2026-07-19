package main

type imageVariant struct {
	Key         string            `json:"key"`
	Label       string            `json:"label"`
	Labels      map[string]string `json:"labels,omitempty"`
	URL         string            `json:"url"`
	Image       string            `json:"image"`
	Cached      bool              `json:"cached"`
	Group       string            `json:"group"`
	GroupLabels map[string]string `json:"group_labels,omitempty"`
}

type detailStat struct {
	Key     string            `json:"key"`
	Name    string            `json:"name"`
	Names   map[string]string `json:"names,omitempty"`
	Value   int               `json:"value"`
	Percent int               `json:"percent"`
}

type detailAbility struct {
	Name         string            `json:"name"`
	Names        map[string]string `json:"names"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Hidden       bool              `json:"hidden"`
}

type evolutionStep struct {
	Card       card              `json:"card"`
	Depth      int               `json:"depth"`
	Conditions map[string]string `json:"conditions,omitempty"`
}

type localizedPokemonText struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Genus       string `json:"genus"`
	Category    string `json:"category"`
	Habitat     string `json:"habitat"`
	Color       string `json:"color"`
	Shape       string `json:"shape"`
	GrowthRate  string `json:"growth_rate"`
	Generation  string `json:"generation"`
	Gender      string `json:"gender"`
}

type pokemonDetail struct {
	Card           card                            `json:"card"`
	Localized      map[string]localizedPokemonText `json:"localized"`
	Images         []imageVariant                  `json:"images"`
	Description    string                          `json:"description"`
	Genus          string                          `json:"genus"`
	Height         string                          `json:"height"`
	Weight         string                          `json:"weight"`
	BaseExperience int                             `json:"base_experience"`
	Abilities      []detailAbility                 `json:"abilities"`
	Stats          []detailStat                    `json:"stats"`
	CaptureRate    int                             `json:"capture_rate"`
	BaseHappiness  int                             `json:"base_happiness"`
	HatchCycles    int                             `json:"hatch_cycles"`
	Gender         string                          `json:"gender"`
	Habitat        string                          `json:"habitat"`
	Color          string                          `json:"color"`
	Shape          string                          `json:"shape"`
	GrowthRate     string                          `json:"growth_rate"`
	Generation     string                          `json:"generation"`
	Category       string                          `json:"category"`
	MovesTotal     int                             `json:"moves_total"`
	GenderRate     int                             `json:"gender_rate"`
	IsBaby         bool                            `json:"is_baby"`
	IsLegendary    bool                            `json:"is_legendary"`
	IsMythical     bool                            `json:"is_mythical"`
	Previous       *card                           `json:"previous,omitempty"`
	Next           *card                           `json:"next,omitempty"`
	Evolution      []evolutionStep                 `json:"evolution,omitempty"`
}

type moveResource struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Accuracy *int   `json:"accuracy"`
	Power    *int   `json:"power"`
	PP       int    `json:"pp"`
	Priority int    `json:"priority"`
	Type     struct {
		Name string `json:"name"`
	} `json:"type"`
	DamageClass struct {
		Name string `json:"name"`
	} `json:"damage_class"`
	Names            []localizedNameEntry `json:"names"`
	Generation       namedResource        `json:"generation"`
	Target           namedResource        `json:"target"`
	LearnedByPokemon []namedResource      `json:"learned_by_pokemon"`
	EffectEntries    []struct {
		Effect      string        `json:"effect"`
		ShortEffect string        `json:"short_effect"`
		Language    namedResource `json:"language"`
	} `json:"effect_entries"`
	FlavorTextEntries []struct {
		FlavorText string        `json:"flavor_text"`
		Language   namedResource `json:"language"`
	} `json:"flavor_text_entries"`
}

type abilityResource struct {
	Name          string               `json:"name"`
	Names         []localizedNameEntry `json:"names"`
	EffectEntries []struct {
		Effect   string        `json:"effect"`
		Language namedResource `json:"language"`
	} `json:"effect_entries"`
	FlavorTextEntries []struct {
		FlavorText string        `json:"flavor_text"`
		Language   namedResource `json:"language"`
	} `json:"flavor_text_entries"`
}

type evolutionChainResource struct {
	Chain evolutionChainLink `json:"chain"`
}

type evolutionChainLink struct {
	Species          namedResource        `json:"species"`
	EvolutionDetails []evolutionDetail    `json:"evolution_details"`
	EvolvesTo        []evolutionChainLink `json:"evolves_to"`
}

type evolutionDetail struct {
	Trigger               namedResource `json:"trigger"`
	Item                  namedResource `json:"item"`
	HeldItem              namedResource `json:"held_item"`
	KnownMove             namedResource `json:"known_move"`
	KnownMoveType         namedResource `json:"known_move_type"`
	Location              namedResource `json:"location"`
	MinLevel              *int          `json:"min_level"`
	MinHappiness          *int          `json:"min_happiness"`
	MinBeauty             *int          `json:"min_beauty"`
	MinAffection          *int          `json:"min_affection"`
	TimeOfDay             string        `json:"time_of_day"`
	NeedsOverworldRain    bool          `json:"needs_overworld_rain"`
	TurnUpsideDown        bool          `json:"turn_upside_down"`
	Gender                *int          `json:"gender"`
	RelativePhysicalStats *int          `json:"relative_physical_stats"`
}

type displayMove struct {
	ID               int               `json:"id"`
	Key              string            `json:"key"`
	Name             string            `json:"name"`
	Names            map[string]string `json:"names"`
	English          string            `json:"english"`
	Type             displayType       `json:"type"`
	DamageClass      string            `json:"damage_class"`
	DamageKey        string            `json:"damage_key"`
	DamageNames      map[string]string `json:"damage_names,omitempty"`
	DamageIcon       string            `json:"damage_icon"`
	Power            string            `json:"power"`
	Accuracy         string            `json:"accuracy"`
	PP               int               `json:"pp"`
	LearnMethod      string            `json:"learn_method"`
	LearnKey         string            `json:"learn_key"`
	LearnMethodNames map[string]string `json:"learn_method_names,omitempty"`
	Level            int               `json:"level"`
	VersionGroup     string            `json:"version_group"`
}

type moveBatch struct {
	Moves      []displayMove `json:"moves"`
	NextOffset int           `json:"next_offset"`
	Total      int           `json:"total"`
	HasMore    bool          `json:"has_more"`
}

type moveDetailResponse struct {
	Move            displayMove       `json:"move"`
	Descriptions    map[string]string `json:"descriptions"`
	Generation      string            `json:"generation"`
	GenerationNames map[string]string `json:"generation_names,omitempty"`
	Target          string            `json:"target"`
	TargetNames     map[string]string `json:"target_names,omitempty"`
	Priority        int               `json:"priority"`
	Pokemon         []card            `json:"pokemon"`
	PokemonTotal    int               `json:"pokemon_total"`
	PokemonNext     int               `json:"pokemon_next"`
	PokemonHasMore  bool              `json:"pokemon_has_more"`
}
