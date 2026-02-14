package store

import "testing"

func TestFuseResults_OrdersByRRFNotDisplaySimilarity(t *testing.T) {
	t.Helper()

	vecResults := []VectorResult{
		{ID: "issue/A", VecScore: 0.90, Type: "issue", Number: 1, Title: "A"},
		{ID: "issue/B", VecScore: 0.89, Type: "issue", Number: 2, Title: "B"},
	}
	ftsResults := []FTSResult{
		{ID: "issue/B", FTSScore: 0.70, Type: "issue", Number: 2, Title: "B"},
	}

	fused := FuseResults(vecResults, ftsResults, "", FuseConfig{
		SimilarityThreshold: 0.50,
		DuplicateThreshold:  0.95,
		MaxResults:          10,
	})

	if len(fused) != 2 {
		t.Fatalf("FuseResults() len = %d, want 2", len(fused))
	}
	if fused[0].ID != "issue/B" {
		t.Fatalf("FuseResults()[0].ID = %s, want issue/B", fused[0].ID)
	}
	if fused[1].ID != "issue/A" {
		t.Fatalf("FuseResults()[1].ID = %s, want issue/A", fused[1].ID)
	}
	if fused[1].DisplaySimilarity <= fused[0].DisplaySimilarity {
		t.Fatalf("expected A to have higher display similarity than B")
	}
}

func TestFuseResults_AppliesThresholdsAndMaxResults(t *testing.T) {
	t.Helper()

	vecResults := []VectorResult{
		{ID: "issue/A", VecScore: 0.95, Type: "issue", Number: 1, Title: "A"},
		{ID: "issue/B", VecScore: 0.80, Type: "issue", Number: 2, Title: "B"},
		{ID: "issue/C", VecScore: 0.60, Type: "issue", Number: 3, Title: "C"},
	}

	fused := FuseResults(vecResults, nil, "", FuseConfig{
		SimilarityThreshold: 0.75,
		DuplicateThreshold:  0.92,
		MaxResults:          2,
	})

	if len(fused) != 2 {
		t.Fatalf("FuseResults() len = %d, want 2", len(fused))
	}
	if fused[0].ID != "issue/A" || !fused[0].IsDuplicate {
		t.Fatalf("first item mismatch: %+v", fused[0])
	}
	if fused[1].ID != "issue/B" || fused[1].IsDuplicate {
		t.Fatalf("second item mismatch: %+v", fused[1])
	}
}

func TestFuseResults_DefensiveSelfExclusion(t *testing.T) {
	t.Helper()

	vecResults := []VectorResult{
		{ID: "issue/self", VecScore: 0.99, Type: "issue", Number: 10, Title: "self"},
		{ID: "issue/other", VecScore: 0.85, Type: "issue", Number: 11, Title: "other"},
	}
	ftsResults := []FTSResult{
		{ID: "issue/self", FTSScore: 0.50, Type: "issue", Number: 10, Title: "self"},
	}

	fused := FuseResults(vecResults, ftsResults, "issue/self", FuseConfig{
		SimilarityThreshold: 0.75,
		DuplicateThreshold:  0.90,
		MaxResults:          5,
	})

	if len(fused) != 1 {
		t.Fatalf("FuseResults() len = %d, want 1", len(fused))
	}
	if fused[0].ID != "issue/other" {
		t.Fatalf("FuseResults()[0].ID = %s, want issue/other", fused[0].ID)
	}
}

func TestFuseResults_UsesFTSOnlyScoreWhenVectorMissing(t *testing.T) {
	t.Helper()

	ftsResults := []FTSResult{
		{ID: "issue/fts", FTSScore: 0.81, Type: "issue", Number: 4, Title: "fts"},
	}

	fused := FuseResults(nil, ftsResults, "", FuseConfig{
		SimilarityThreshold: 0.75,
		DuplicateThreshold:  0.92,
		MaxResults:          5,
	})

	if len(fused) != 1 {
		t.Fatalf("FuseResults() len = %d, want 1", len(fused))
	}
	if fused[0].VecScore != 0 {
		t.Fatalf("VecScore = %f, want 0", fused[0].VecScore)
	}
	if fused[0].FTSScore != 0.81 {
		t.Fatalf("FTSScore = %f, want 0.81", fused[0].FTSScore)
	}
	if fused[0].DisplaySimilarity != 0.81 {
		t.Fatalf("DisplaySimilarity = %f, want 0.81", fused[0].DisplaySimilarity)
	}
}

func TestFuseResults_ClampsScoresToUnitInterval(t *testing.T) {
	t.Helper()

	vecResults := []VectorResult{
		{ID: "issue/high", VecScore: 1.4, Type: "issue", Number: 1, Title: "high"},
		{ID: "issue/low", VecScore: -0.2, Type: "issue", Number: 2, Title: "low"},
	}
	ftsResults := []FTSResult{
		{ID: "issue/high", FTSScore: 1.7, Type: "issue", Number: 1, Title: "high"},
		{ID: "issue/low", FTSScore: -0.5, Type: "issue", Number: 2, Title: "low"},
	}

	fused := FuseResults(vecResults, ftsResults, "", FuseConfig{
		SimilarityThreshold: 0.1,
		DuplicateThreshold:  0.9,
		MaxResults:          10,
	})

	if len(fused) != 1 {
		t.Fatalf("FuseResults() len = %d, want 1 (low item filtered after clamping)", len(fused))
	}
	if fused[0].DisplaySimilarity != 1 {
		t.Fatalf("DisplaySimilarity = %f, want 1", fused[0].DisplaySimilarity)
	}
}
