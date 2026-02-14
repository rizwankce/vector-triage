package store

import "sort"

const (
	defaultSimilarityThreshold = 0.75
	defaultDuplicateThreshold  = 0.92
	defaultMaxResults          = 5
	rrfK                       = 60
)

// FuseConfig controls thresholding and truncation for fused results.
type FuseConfig struct {
	SimilarityThreshold float64
	DuplicateThreshold  float64
	MaxResults          int
}

// FusedResult is the merged ranking output from vector and FTS backends.
type FusedResult struct {
	ID                string
	Type              string
	Number            int
	Title             string
	State             string
	URL               string
	RRFScore          float64
	VecScore          float64
	FTSScore          float64
	DisplaySimilarity float64
	IsDuplicate       bool
}

type fusedAccumulator struct {
	ID       string
	Type     string
	Number   int
	Title    string
	State    string
	URL      string
	RRFScore float64
	VecScore float64
	FTSScore float64
}

func (c FuseConfig) normalized() FuseConfig {
	out := FuseConfig{
		SimilarityThreshold: c.SimilarityThreshold,
		DuplicateThreshold:  c.DuplicateThreshold,
		MaxResults:          c.MaxResults,
	}

	if c == (FuseConfig{}) {
		return FuseConfig{
			SimilarityThreshold: defaultSimilarityThreshold,
			DuplicateThreshold:  defaultDuplicateThreshold,
			MaxResults:          defaultMaxResults,
		}
	}

	if out.MaxResults <= 0 {
		out.MaxResults = defaultMaxResults
	}

	out.SimilarityThreshold = clamp01(out.SimilarityThreshold)
	out.DuplicateThreshold = clamp01(out.DuplicateThreshold)
	return out
}

// FuseResults applies RRF ordering while using max(vecScore, ftsScore) as user-facing similarity.
func FuseResults(vecResults []VectorResult, ftsResults []FTSResult, excludeID string, config FuseConfig) []FusedResult {
	cfg := config.normalized()
	acc := map[string]*fusedAccumulator{}

	vecSeen := map[string]struct{}{}
	for rank, item := range vecResults {
		if item.ID == "" || item.ID == excludeID {
			continue
		}
		if _, exists := vecSeen[item.ID]; exists {
			continue
		}
		vecSeen[item.ID] = struct{}{}

		current := getOrCreateAccumulator(acc, item.ID)
		mergeMetadata(current, item.Type, item.Number, item.Title, item.State, item.URL)
		current.VecScore = maxFloat(current.VecScore, clamp01(item.VecScore))
		current.RRFScore += 1.0 / float64(rrfK+rank+1)
	}

	ftsSeen := map[string]struct{}{}
	for rank, item := range ftsResults {
		if item.ID == "" || item.ID == excludeID {
			continue
		}
		if _, exists := ftsSeen[item.ID]; exists {
			continue
		}
		ftsSeen[item.ID] = struct{}{}

		current := getOrCreateAccumulator(acc, item.ID)
		mergeMetadata(current, item.Type, item.Number, item.Title, item.State, item.URL)
		current.FTSScore = maxFloat(current.FTSScore, clamp01(item.FTSScore))
		current.RRFScore += 1.0 / float64(rrfK+rank+1)
	}

	fused := make([]FusedResult, 0, len(acc))
	for _, item := range acc {
		displaySimilarity := maxFloat(item.VecScore, item.FTSScore)
		if displaySimilarity < cfg.SimilarityThreshold {
			continue
		}

		fused = append(fused, FusedResult{
			ID:                item.ID,
			Type:              item.Type,
			Number:            item.Number,
			Title:             item.Title,
			State:             item.State,
			URL:               item.URL,
			RRFScore:          item.RRFScore,
			VecScore:          item.VecScore,
			FTSScore:          item.FTSScore,
			DisplaySimilarity: displaySimilarity,
			IsDuplicate:       displaySimilarity >= cfg.DuplicateThreshold,
		})
	}

	sort.Slice(fused, func(i, j int) bool {
		if fused[i].RRFScore == fused[j].RRFScore {
			if fused[i].DisplaySimilarity == fused[j].DisplaySimilarity {
				return fused[i].ID < fused[j].ID
			}
			return fused[i].DisplaySimilarity > fused[j].DisplaySimilarity
		}
		return fused[i].RRFScore > fused[j].RRFScore
	})

	if len(fused) > cfg.MaxResults {
		fused = fused[:cfg.MaxResults]
	}

	return fused
}

func getOrCreateAccumulator(acc map[string]*fusedAccumulator, id string) *fusedAccumulator {
	current, ok := acc[id]
	if ok {
		return current
	}

	current = &fusedAccumulator{ID: id}
	acc[id] = current
	return current
}

func mergeMetadata(target *fusedAccumulator, typ string, number int, title, state, url string) {
	if target.Type == "" {
		target.Type = typ
	}
	if target.Number == 0 {
		target.Number = number
	}
	if target.Title == "" {
		target.Title = title
	}
	if target.State == "" {
		target.State = state
	}
	if target.URL == "" {
		target.URL = url
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
