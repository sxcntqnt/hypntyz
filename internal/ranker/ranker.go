package ranker

import "sort"

type Item struct {
	ID    string
	Score float64
}

func TopK(items []Item, k int) []Item {

	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	if len(items) < k {
		return items
	}

	return items[:k]
}
