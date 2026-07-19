package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func mediaURL(src string) string {
	if src == "" {
		return "/placeholder.svg"
	}
	return "/media/image?src=" + url.QueryEscape(src)
}

func pokemonImages(p pokemon) []imageVariant {
	seen := make(map[string]bool)
	images := make([]imageVariant, 0, 32)
	add := func(key, label, src string, cached bool) {
		if src == "" || seen[src] {
			return
		}
		seen[src] = true
		image := src
		if cached {
			image = mediaURL(src)
		}
		group, groupLabels := imageGroup(key)
		images = append(images, imageVariant{Key: key, Label: label, Labels: localizedImageLabels(label, cached), URL: src, Image: image, Cached: cached, Group: group, GroupLabels: groupLabels})
	}
	addSet := func(prefix, title string, s spriteSet) {
		add(prefix+"-front", title+" · 正面", s.FrontDefault, true)
		add(prefix+"-back", title+" · 背面", s.BackDefault, true)
		add(prefix+"-front-female", title+" · 雌性正面", s.FrontFemale, true)
		add(prefix+"-back-female", title+" · 雌性背面", s.BackFemale, true)
		add(prefix+"-front-shiny", title+" · 闪光正面", s.FrontShiny, true)
		add(prefix+"-back-shiny", title+" · 闪光背面", s.BackShiny, true)
		add(prefix+"-front-shiny-female", title+" · 闪光雌性正面", s.FrontShinyFemale, true)
		add(prefix+"-back-shiny-female", title+" · 闪光雌性背面", s.BackShinyFemale, true)
	}
	addSet("official", "官方立绘", p.Sprites.Other.Official)
	addSet("home", "Pokémon HOME", p.Sprites.Other.Home)
	addSet("dream-world", "Dream World", p.Sprites.Other.Dream)
	addSet("showdown", "Showdown 动画", p.Sprites.Other.Showdown)
	addSet("classic", "经典像素", p.Sprites.spriteSet)

	generations := make([]string, 0, len(p.Sprites.Versions))
	for generation := range p.Sprites.Versions {
		generations = append(generations, generation)
	}
	sort.Strings(generations)
	for _, generation := range generations {
		var node any
		if json.Unmarshal(p.Sprites.Versions[generation], &node) == nil {
			collectVersionSprites(node, []string{generation}, func(key, label, src string) {
				add(key, label, src, false)
			})
		}
	}
	return images
}

func imageGroup(key string) (string, map[string]string) {
	groups := []struct {
		prefix, key, zh, en, ja string
	}{
		{"official-", "official", "官方立绘", "Official artwork", "公式アート"},
		{"home-", "home", "Pokémon HOME", "Pokémon HOME", "Pokémon HOME"},
		{"dream-world-", "dream-world", "Dream World", "Dream World", "ドリームワールド"},
		{"showdown-", "showdown", "Showdown 动画", "Showdown animation", "Showdown アニメ"},
		{"classic-", "classic", "经典像素", "Classic sprites", "クラシックスプライト"},
	}
	for _, group := range groups {
		if strings.HasPrefix(key, group.prefix) {
			return group.key, map[string]string{"zh": group.zh, "en": group.en, "ja": group.ja}
		}
	}
	return "games", map[string]string{"zh": "历代游戏", "en": "Game generations", "ja": "歴代ゲーム"}
}

func localizedImageLabels(label string, cached bool) map[string]string {
	en := strings.NewReplacer("官方立绘", "Official artwork", "Pokémon HOME", "Pokémon HOME", "Dream World", "Dream World", "Showdown 动画", "Showdown animation", "经典像素", "Classic sprite", "闪光雌性正面", "Shiny female front", "闪光雌性背面", "Shiny female back", "雌性正面", "Female front", "雌性背面", "Female back", "闪光正面", "Shiny front", "闪光背面", "Shiny back", "正面", "Front", "背面", "Back", "历代图像", "Game sprites").Replace(label)
	ja := strings.NewReplacer("官方立绘", "公式アート", "Pokémon HOME", "Pokémon HOME", "Dream World", "ドリームワールド", "Showdown 动画", "Showdown アニメ", "经典像素", "クラシックスプライト", "闪光雌性正面", "色違いメス正面", "闪光雌性背面", "色違いメス背面", "雌性正面", "メス正面", "雌性背面", "メス背面", "闪光正面", "色違い正面", "闪光背面", "色違い背面", "正面", "正面", "背面", "背面", "历代图像", "歴代画像").Replace(label)
	return map[string]string{"zh": label, "en": en, "ja": ja}
}

func collectVersionSprites(node any, path []string, add func(string, string, string)) {
	switch value := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectVersionSprites(value[key], append(path, key), add)
		}
	case string:
		if strings.HasPrefix(value, "https://") {
			key := strings.Join(path, "-")
			label := "历代图像 · " + strings.Join(path, " / ")
			add(key, label, value)
		}
	}
}

func allLocalizedPokemonTexts(p pokemon, s species) map[string]localizedPokemonText {
	texts := make(map[string]localizedPokemonText)
	for _, entry := range s.Names {
		text := texts[entry.Language.Name]
		text.Name = entry.Name
		texts[entry.Language.Name] = text
	}
	for _, entry := range s.FlavorTextEntries {
		text := texts[entry.Language.Name]
		if text.Description == "" {
			text.Description = strings.Join(strings.Fields(entry.FlavorText), " ")
		}
		texts[entry.Language.Name] = text
	}
	for _, entry := range s.Genera {
		text := texts[entry.Language.Name]
		text.Genus = entry.Genus
		text.Category = entry.Genus
		texts[entry.Language.Name] = text
	}
	english := texts["en"]
	if english.Name == "" {
		english.Name = p.Name
	}
	texts["en"] = english
	return texts
}

func mergeLocalizedResource(texts map[string]localizedPokemonText, names map[string]string, field string) {
	for language, value := range names {
		text := texts[language]
		switch field {
		case "habitat":
			text.Habitat = value
		case "color":
			text.Color = value
		case "shape":
			text.Shape = value
		case "growth-rate":
			text.GrowthRate = value
		case "generation":
			text.Generation = value
		}
		texts[language] = text
	}
}

func fallbackResourceNames(field, key string) map[string]string {
	type labels struct{ zh, en, ja string }
	resources := map[string]map[string]labels{
		"habitat": {
			"cave": {"洞穴", "Cave", "洞窟"}, "forest": {"森林", "Forest", "森"}, "grassland": {"草原", "Grassland", "草原"},
			"mountain": {"山地", "Mountain", "山岳"}, "rare": {"稀有地点", "Rare", "珍しい場所"}, "rough-terrain": {"崎岖地带", "Rough terrain", "荒地"},
			"sea": {"海洋", "Sea", "海"}, "urban": {"城市", "Urban", "都市"}, "waters-edge": {"水边", "Water's edge", "水辺"},
		},
		"growth-rate": {
			"slow": {"慢", "Slow", "遅い"}, "medium": {"中等", "Medium", "普通"}, "fast": {"快", "Fast", "速い"},
			"medium-slow": {"中等偏慢", "Medium slow", "やや遅い"}, "slow-then-very-fast": {"先慢后极快", "Slow then very fast", "初めは遅く後に非常に速い"},
			"fast-then-very-slow": {"先快后极慢", "Fast then very slow", "初めは速く後に非常に遅い"},
		},
		"shape": {
			"ball": {"球形", "Ball", "球形"}, "squiggle": {"蛇形", "Squiggle", "蛇形"}, "fish": {"鱼形", "Fish", "魚形"}, "arms": {"双臂形", "Arms", "腕形"},
			"blob": {"团块形", "Blob", "塊形"}, "upright": {"直立形", "Upright", "直立形"}, "legs": {"多足形", "Legs", "脚形"}, "quadruped": {"四足形", "Quadruped", "四足形"},
			"wings": {"双翼形", "Wings", "翼形"}, "tentacles": {"触手形", "Tentacles", "触手形"}, "heads": {"多头形", "Heads", "多頭形"},
			"humanoid": {"人形", "Humanoid", "人型"}, "bug-wings": {"虫翼形", "Bug wings", "虫の羽形"}, "armor": {"甲壳形", "Armor", "装甲形"},
		},
	}
	label, ok := resources[field][key]
	if !ok {
		return nil
	}
	return map[string]string{"zh": label.zh, "zh-hans": label.zh, "zh-hant": label.zh, "en": label.en, "ja": label.ja, "ja-hrkt": label.ja}
}

func fillMissingNames(names, fallback map[string]string) map[string]string {
	if names == nil {
		names = make(map[string]string)
	}
	for language, value := range fallback {
		if names[language] == "" {
			names[language] = value
		}
	}
	return names
}

func localizedAbilityDescriptions(resource abilityResource) map[string]string {
	descriptions := make(map[string]string)
	for _, entry := range resource.EffectEntries {
		if entry.Language.Name != "" && entry.Effect != "" {
			descriptions[entry.Language.Name] = strings.Join(strings.Fields(entry.Effect), " ")
		}
	}
	for _, entry := range resource.FlavorTextEntries {
		if entry.Language.Name != "" && descriptions[entry.Language.Name] == "" {
			descriptions[entry.Language.Name] = strings.Join(strings.Fields(entry.FlavorText), " ")
		}
	}
	return descriptions
}

func evolutionConditions(detail evolutionDetail) map[string]string {
	pretty := func(value string) string { return strings.ReplaceAll(value, "-", " ") }
	zh, en, ja := []string{}, []string{}, []string{}
	add := func(z, e, j string) { zh, en, ja = append(zh, z), append(en, e), append(ja, j) }
	if detail.MinLevel != nil {
		level := strconv.Itoa(*detail.MinLevel)
		add("等级 "+level, "Level "+level, "レベル "+level)
	}
	if detail.Item.Name != "" {
		item := pretty(detail.Item.Name)
		add("使用 "+item, "Use "+item, item+"を使う")
	}
	if detail.HeldItem.Name != "" {
		item := pretty(detail.HeldItem.Name)
		add("携带 "+item, "Hold "+item, item+"を持たせる")
	}
	if detail.MinHappiness != nil {
		value := strconv.Itoa(*detail.MinHappiness)
		add("亲密度 ≥ "+value, "Friendship ≥ "+value, "なかよし度 ≥ "+value)
	}
	if detail.MinAffection != nil {
		value := strconv.Itoa(*detail.MinAffection)
		add("友好度 ≥ "+value, "Affection ≥ "+value, "なかよし度 ≥ "+value)
	}
	if detail.MinBeauty != nil {
		value := strconv.Itoa(*detail.MinBeauty)
		add("美丽度 ≥ "+value, "Beauty ≥ "+value, "うつくしさ ≥ "+value)
	}
	if detail.TimeOfDay != "" {
		values := map[string][3]string{"day": {"白天", "Day", "昼"}, "night": {"夜晚", "Night", "夜"}}
		value := values[detail.TimeOfDay]
		if value[0] == "" {
			value = [3]string{pretty(detail.TimeOfDay), pretty(detail.TimeOfDay), pretty(detail.TimeOfDay)}
		}
		add(value[0], value[1], value[2])
	}
	if detail.KnownMove.Name != "" {
		move := pretty(detail.KnownMove.Name)
		add("学会 "+move, "Know "+move, move+"を覚える")
	}
	if detail.KnownMoveType.Name != "" {
		kind := pretty(detail.KnownMoveType.Name)
		add("学会 "+kind+" 属性招式", "Know a "+kind+"-type move", kind+"タイプの技を覚える")
	}
	if detail.Location.Name != "" {
		place := pretty(detail.Location.Name)
		add("地点："+place, "At "+place, "場所："+place)
	}
	if detail.NeedsOverworldRain {
		add("下雨时", "While raining", "雨の時")
	}
	if detail.TurnUpsideDown {
		add("倒置设备", "Turn device upside down", "本体を逆さにする")
	}
	if len(en) == 0 && detail.Trigger.Name != "" {
		trigger := pretty(detail.Trigger.Name)
		add(trigger, trigger, trigger)
	}
	return map[string]string{"zh": strings.Join(zh, " · "), "en": strings.Join(en, " · "), "ja": strings.Join(ja, " · ")}
}

func (a *app) buildEvolution(ctx context.Context, link evolutionChainLink, depth int, conditions map[string]string, out *[]evolutionStep) {
	c, err := a.getCard(ctx, link.Species.Name)
	if err != nil {
		c = card{Name: link.Species.Name, English: link.Species.Name, Image: "/placeholder.svg", Names: map[string]string{"en": link.Species.Name}}
	}
	*out = append(*out, evolutionStep{Card: c, Depth: depth, Conditions: conditions})
	for _, child := range link.EvolvesTo {
		childConditions := map[string]string(nil)
		if len(child.EvolutionDetails) > 0 {
			childConditions = evolutionConditions(child.EvolutionDetails[0])
		}
		a.buildEvolution(ctx, child, depth+1, childConditions, out)
	}
}

func makePokemonDetail(p pokemon, s species) pokemonDetail {
	localized := allLocalizedPokemonTexts(p, s)
	defaultText := localized["zh-hans"]
	if defaultText.Name == "" {
		defaultText = localized["en"]
	}
	d := pokemonDetail{
		Card:           makeCard(p, s),
		Localized:      localized,
		Images:         pokemonImages(p),
		Description:    defaultText.Description,
		Genus:          defaultText.Genus,
		Height:         fmt.Sprintf("%.1f m", float64(p.Height)/10),
		Weight:         fmt.Sprintf("%.1f kg", float64(p.Weight)/10),
		BaseExperience: p.BaseExperience,
		CaptureRate:    s.CaptureRate,
		BaseHappiness:  s.BaseHappiness,
		HatchCycles:    s.HatchCounter,
		GenderRate:     s.GenderRate,
		IsBaby:         s.IsBaby,
		IsLegendary:    s.IsLegendary,
		IsMythical:     s.IsMythical,
		Habitat:        defaultText.Habitat,
		Color:          defaultText.Color,
		Shape:          defaultText.Shape,
		GrowthRate:     defaultText.GrowthRate,
		Generation:     defaultText.Generation,
		Category:       defaultText.Category,
		MovesTotal:     len(p.Moves),
	}
	for _, ability := range p.Abilities {
		fallback := strings.ReplaceAll(ability.Ability.Name, "-", " ")
		d.Abilities = append(d.Abilities, detailAbility{Name: fallback, Names: map[string]string{"zh": fallback, "en": fallback, "ja": fallback}, Hidden: ability.IsHidden})
	}
	for _, stat := range p.Stats {
		percent := stat.Base * 100 / 255
		if percent > 100 {
			percent = 100
		}
		d.Stats = append(d.Stats, detailStat{Key: stat.Stat.Name, Name: stat.Stat.Name, Names: map[string]string{"en": stat.Stat.Name}, Value: stat.Base, Percent: percent})
	}
	return d
}

func (a *app) getPokemonDetail(ctx context.Context, name string) (pokemonDetail, error) {
	var p pokemon
	if err := a.api(ctx, "/pokemon/"+url.PathEscape(strings.ToLower(name)), &p); err != nil {
		return pokemonDetail{}, err
	}
	var s species
	if err := a.api(ctx, "/pokemon-species/"+strconv.Itoa(p.ID), &s); err != nil {
		return pokemonDetail{}, err
	}
	detail := makePokemonDetail(p, s)
	a.localizeTypes(ctx, detail.Card.Types)
	resources := []struct {
		field string
		path  string
		key   string
	}{
		{"habitat", "/pokemon-habitat/" + url.PathEscape(s.Habitat.Name), s.Habitat.Name},
		{"color", "/pokemon-color/" + url.PathEscape(s.Color.Name), s.Color.Name},
		{"shape", "/pokemon-shape/" + url.PathEscape(s.Shape.Name), s.Shape.Name},
		{"growth-rate", "/growth-rate/" + url.PathEscape(s.GrowthRate.Name), s.GrowthRate.Name},
		{"generation", "/generation/" + url.PathEscape(s.Generation.Name), s.Generation.Name},
	}
	resourceNames := make([]map[string]string, len(resources))
	var wg sync.WaitGroup
	if s.EvolutionChain.URL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var chain evolutionChainResource
			path := strings.TrimPrefix(s.EvolutionChain.URL, pokeAPI)
			if a.api(ctx, path, &chain) == nil {
				a.buildEvolution(ctx, chain.Chain, 0, nil, &detail.Evolution)
			}
		}()
	}
	for i, resource := range resources {
		if strings.HasSuffix(resource.path, "/") {
			continue
		}
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			resourceNames[i], _ = a.translatedNames(ctx, path)
			resourceNames[i] = fillMissingNames(resourceNames[i], fallbackResourceNames(resources[i].field, resources[i].key))
		}(i, resource.path)
	}
	for i := range detail.Stats {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if names, err := a.translatedNames(ctx, "/stat/"+url.PathEscape(detail.Stats[i].Key)); err == nil {
				detail.Stats[i].Names = names
				detail.Stats[i].Name = preferredLocalizedName(names, detail.Stats[i].Key, "zh-hans", "zh-hant", "en")
			}
		}(i)
	}
	for i, ability := range p.Abilities {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			var resource abilityResource
			if a.api(ctx, "/ability/"+url.PathEscape(name), &resource) != nil {
				return
			}
			fallback := strings.ReplaceAll(name, "-", " ")
			names := namesMap(resource.Names, "en", fallback)
			detail.Abilities[i].Names = names
			detail.Abilities[i].Name = preferredLocalizedName(names, fallback, "zh-hans", "zh-hant", "en")
			detail.Abilities[i].Descriptions = localizedAbilityDescriptions(resource)
		}(i, ability.Ability.Name)
	}
	wg.Wait()
	for i, resource := range resources {
		mergeLocalizedResource(detail.Localized, resourceNames[i], resource.field)
	}
	defaultText := detail.Localized["zh-hans"]
	if defaultText.Name == "" {
		defaultText = detail.Localized["en"]
	}
	detail.Description, detail.Genus, detail.Category = defaultText.Description, defaultText.Genus, defaultText.Category
	detail.Habitat, detail.Color, detail.Shape = defaultText.Habitat, defaultText.Color, defaultText.Shape
	detail.GrowthRate, detail.Generation = defaultText.GrowthRate, defaultText.Generation
	previous, next := a.catalogNeighbors(p.Name)
	detail.Previous, detail.Next = previous, next
	return detail, nil
}

func (a *app) catalogNeighbors(name string) (previous, next *card) {
	a.catalogMu.RLock()
	items := append([]listItem(nil), a.catalog...)
	a.catalogMu.RUnlock()
	index := -1
	for i, item := range items {
		if item.Name == name {
			index = i
			break
		}
	}
	makeNeighbor := func(item listItem) *card {
		c, ok := a.cachedCard(item.Name)
		if !ok {
			c = card{ID: resultID(item.URL), Name: item.Name, English: item.Name, Image: "/placeholder.svg"}
		}
		return &c
	}
	if index > 0 {
		previous = makeNeighbor(items[index-1])
	}
	if index >= 0 && index+1 < len(items) {
		next = makeNeighbor(items[index+1])
	}
	return previous, next
}

func (a *app) pokemonDetailPage(w http.ResponseWriter, r *http.Request) {
	a.renderIndex(w, r, r.PathValue("name"))
}

func (a *app) pokemonDetailAPI(w http.ResponseWriter, r *http.Request) {
	detail, err := a.getPokemonDetail(r.Context(), r.PathValue("name"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		http.Error(w, `{"error":"pokemon unavailable"}`, http.StatusNotFound)
		return
	}
	go func(images []imageVariant) {
		for _, image := range images {
			if image.Cached {
				a.enqueueImageBackground(image.URL)
			}
		}
	}(append([]imageVariant(nil), detail.Images...))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(detail)
}

func latestMoveLearning(pm pokemonMove) (method string, level int, version string) {
	if len(pm.VersionGroupDetails) == 0 {
		return "unknown", 0, ""
	}
	best := len(pm.VersionGroupDetails) - 1
	bestID := 0
	for i, candidate := range pm.VersionGroupDetails {
		// These two original Japanese releases were appended to PokéAPI much
		// later and therefore have high IDs despite being first-generation data.
		if candidate.VersionGroup.Name == "red-green-japan" || candidate.VersionGroup.Name == "blue-japan" {
			continue
		}
		if id := resultID(candidate.VersionGroup.URL); id > bestID {
			best, bestID = i, id
		}
	}
	detail := pm.VersionGroupDetails[best]
	return detail.MoveLearnMethod.Name, detail.LevelLearnedAt, detail.VersionGroup.Name
}

func localizedMoveNames(m moveResource) map[string]string {
	return namesMap(m.Names, "en", strings.ReplaceAll(m.Name, "-", " "))
}

func optionalNumber(value *int) string {
	if value == nil {
		return "—"
	}
	return strconv.Itoa(*value)
}

func makeDisplayMove(m moveResource, pm pokemonMove, damageNames, methodNames map[string]string) displayMove {
	method, level, version := latestMoveLearning(pm)
	damageIcons := map[string]string{"physical": "✹", "special": "◎", "status": "◌"}
	names := localizedMoveNames(m)
	damageFallback := strings.ReplaceAll(m.DamageClass.Name, "-", " ")
	methodFallback := strings.ReplaceAll(method, "-", " ")
	return displayMove{
		ID:               m.ID,
		Key:              m.Name,
		Name:             preferredLocalizedName(names, m.Name, "zh-hans", "zh-hant", "en"),
		Names:            names,
		English:          preferredLocalizedName(names, m.Name, "en"),
		Type:             typeDisplay(m.Type.Name),
		DamageClass:      preferredLocalizedName(damageNames, damageFallback, "zh-hans", "zh-hant", "en"),
		DamageKey:        m.DamageClass.Name,
		DamageNames:      damageNames,
		DamageIcon:       damageIcons[m.DamageClass.Name],
		Power:            optionalNumber(m.Power),
		Accuracy:         optionalNumber(m.Accuracy),
		PP:               m.PP,
		LearnMethod:      preferredLocalizedName(methodNames, methodFallback, "zh-hans", "zh-hant", "en"),
		LearnKey:         method,
		LearnMethodNames: methodNames,
		Level:            level,
		VersionGroup:     version,
	}
}

func moveOrder(pm pokemonMove) (int, int, string) {
	method, level, _ := latestMoveLearning(pm)
	priority := map[string]int{"level-up": 0, "machine": 1, "egg": 2, "tutor": 3, "form-change": 4}[method]
	if priority == 0 && method != "level-up" {
		priority = 5
	}
	return priority, level, pm.Move.Name
}

func (a *app) movesAPI(w http.ResponseWriter, r *http.Request) {
	var p pokemon
	if err := a.api(r.Context(), "/pokemon/"+url.PathEscape(strings.ToLower(r.PathValue("name"))), &p); err != nil {
		http.Error(w, `{"error":"pokemon unavailable"}`, http.StatusBadGateway)
		return
	}
	moves := append([]pokemonMove(nil), p.Moves...)
	sort.SliceStable(moves, func(i, j int) bool {
		pi, li, ni := moveOrder(moves[i])
		pj, lj, nj := moveOrder(moves[j])
		if pi != pj {
			return pi < pj
		}
		if li != lj {
			return li < lj
		}
		return ni < nj
	})
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 250 {
		limit = 24
	}
	if offset > len(moves) {
		offset = len(moves)
	}
	end := min(offset+limit, len(moves))
	selected := moves[offset:end]
	result := make([]displayMove, len(selected))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, pm := range selected {
		wg.Add(1)
		go func(i int, pm pokemonMove) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var move moveResource
			if err := a.api(r.Context(), "/move/"+url.PathEscape(pm.Move.Name), &move); err != nil {
				method, level, version := latestMoveLearning(pm)
				fallback := strings.ReplaceAll(pm.Move.Name, "-", " ")
				methodFallback := strings.ReplaceAll(method, "-", " ")
				result[i] = displayMove{Key: pm.Move.Name, Name: fallback, Names: map[string]string{"en": fallback}, English: fallback, Type: typeDisplay("normal"), DamageClass: "unknown", DamageKey: "unknown", Power: "—", Accuracy: "—", LearnMethod: methodFallback, LearnKey: method, Level: level, VersionGroup: version}
				return
			}
			method, _, _ := latestMoveLearning(pm)
			damageNames, _ := a.translatedNames(r.Context(), "/move-damage-class/"+url.PathEscape(move.DamageClass.Name))
			methodNames, _ := a.translatedNames(r.Context(), "/move-learn-method/"+url.PathEscape(method))
			result[i] = makeDisplayMove(move, pm, damageNames, methodNames)
			types := []displayType{result[i].Type}
			a.localizeTypes(r.Context(), types)
			result[i].Type = types[0]
		}(i, pm)
	}
	wg.Wait()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(moveBatch{Moves: result, NextOffset: end, Total: len(moves), HasMore: end < len(moves)})
}
