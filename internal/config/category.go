package config

import (
	"sort"
	"strings"
)

// Category names, in their fixed display order.
const (
	CategoryAutomatic = "Automatic"
	CategoryRetoia    = "Retoia"
	CategoryByEffort  = "By effort"
	CategoryCloud     = "Cloud"
)

// effortSuffixPrefix is stripped from an "esfuerzo-<level>" model id to
// resolve its effort level for within-category ordering.
const effortSuffixPrefix = "esfuerzo-"

// cloudPrefix identifies a cloud-hosted model id.
const cloudPrefix = "cloud-"

// effortRank orders known effort levels from lowest to highest.
var effortRank = map[string]int{
	"bajo":   0,
	"medio":  1,
	"alto":   2,
	"maximo": 3,
}

// Category classifies a model by its id:
//   - "auto" is Automatic.
//   - an "esfuerzo-" prefix is By effort.
//   - a "cloud-" prefix is Cloud.
//   - anything else is Retoia.
func (m Model) Category() string {
	if m.CategoryLabel != "" {
		return m.CategoryLabel
	}
	switch {
	case m.ID == "auto":
		return CategoryAutomatic
	case strings.HasPrefix(m.ID, effortSuffixPrefix):
		return CategoryByEffort
	case strings.HasPrefix(m.ID, cloudPrefix):
		return CategoryCloud
	default:
		return CategoryRetoia
	}
}

// CategoryOrder returns the fixed display order of categories.
func CategoryOrder() []string {
	return []string{CategoryAutomatic, CategoryRetoia, CategoryByEffort, CategoryCloud}
}

// effortLevelRank maps a model id's "esfuerzo-<level>" suffix to its sort
// rank. An unknown or missing suffix ranks after every known level.
func effortLevelRank(id string) int {
	level := strings.TrimPrefix(id, effortSuffixPrefix)
	if rank, ok := effortRank[level]; ok {
		return rank
	}
	return len(effortRank)
}

// SortModelsInCategory sorts models in place according to the ordering
// rule for the given category: By effort orders by explicit level
// (bajo < medio < alto < maximo, unknown levels last), every other
// category orders alphabetically by id.
func SortModelsInCategory(category string, models []Model) {
	sort.SliceStable(models, func(i, j int) bool {
		if category == CategoryByEffort {
			ri, rj := effortLevelRank(models[i].ID), effortLevelRank(models[j].ID)
			if ri != rj {
				return ri < rj
			}
		}
		return models[i].ID < models[j].ID
	})
}

// ModelGroup is one category's models, already ordered per
// SortModelsInCategory.
type ModelGroup struct {
	Category string
	Models   []Model
}

// GroupModelsByCategory buckets models by Category, sorts each bucket with
// SortModelsInCategory, and returns the groups in the fixed CategoryOrder,
// omitting any category with no models.
func GroupModelsByCategory(models []Model) []ModelGroup {
	byCategory := make(map[string][]Model)
	for _, m := range models {
		cat := m.Category()
		byCategory[cat] = append(byCategory[cat], m)
	}

	var groups []ModelGroup
	known := make(map[string]bool)
	for _, cat := range CategoryOrder() {
		known[cat] = true
		ms := byCategory[cat]
		if len(ms) == 0 {
			continue
		}
		SortModelsInCategory(cat, ms)
		groups = append(groups, ModelGroup{Category: cat, Models: ms})
	}

	// Any category not in the fixed order (custom providers) comes after the
	// built-ins, alphabetically, so added providers show as their own group.
	var extra []string
	for cat := range byCategory {
		if !known[cat] {
			extra = append(extra, cat)
		}
	}
	sort.Strings(extra)
	for _, cat := range extra {
		ms := byCategory[cat]
		SortModelsInCategory(cat, ms)
		groups = append(groups, ModelGroup{Category: cat, Models: ms})
	}
	return groups
}
