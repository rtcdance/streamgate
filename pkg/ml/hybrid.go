package ml

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// HybridRecommender combines multiple recommendation algorithms
type HybridRecommender struct {
	mu                  sync.RWMutex
	collaborativeWeight float64
	contentBasedWeight  float64
	trendingWeight      float64
	personalizedWeight  float64
	diversityFactor     float64
	trendingContent     map[string]float64
	personalizedScores  map[string]map[string]float64
	lastUpdate          time.Time
}

// NewHybridRecommender creates a new hybrid recommender
func NewHybridRecommender() *HybridRecommender {
	return &HybridRecommender{
		collaborativeWeight: 0.4,
		contentBasedWeight:  0.4,
		trendingWeight:      0.1,
		personalizedWeight:  0.1,
		diversityFactor:     0.2,
		trendingContent:     make(map[string]float64),
		personalizedScores:  make(map[string]map[string]float64),
	}
}

// CombineRecommendations combines recommendations from multiple algorithms
func (hr *HybridRecommender) CombineRecommendations(
	collaborativeRecs []*Recommendation,
	contentBasedRecs []*Recommendation,
	collaborativeWeight float64,
	contentBasedWeight float64,
) []*Recommendation {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Normalize weights
	totalWeight := collaborativeWeight + contentBasedWeight
	if totalWeight == 0 {
		return nil
	}

	collaborativeWeight /= totalWeight
	contentBasedWeight /= totalWeight

	// Create score map
	scores := make(map[string]float64)
	confidences := make(map[string]float64)
	reasons := make(map[string]string)
	algorithms := make(map[string]string)

	// Add collaborative filtering scores
	for _, rec := range collaborativeRecs {
		scores[rec.ContentID] += rec.Score * collaborativeWeight
		confidences[rec.ContentID] = rec.Confidence
		reasons[rec.ContentID] = rec.Reason
		algorithms[rec.ContentID] = "hybrid_collaborative"
	}

	// Add content-based scores
	for _, rec := range contentBasedRecs {
		scores[rec.ContentID] += rec.Score * contentBasedWeight
		if _, exists := confidences[rec.ContentID]; !exists {
			confidences[rec.ContentID] = rec.Confidence
		} else {
			// Average confidence
			confidences[rec.ContentID] = (confidences[rec.ContentID] + rec.Confidence) / 2
		}
		if _, exists := reasons[rec.ContentID]; !exists {
			reasons[rec.ContentID] = rec.Reason
		}
		algorithms[rec.ContentID] = "hybrid"
	}

	// Convert to recommendations
	recommendations := make([]*Recommendation, 0)
	for contentID, score := range scores {
		recommendations = append(recommendations, &Recommendation{
			ContentID:  contentID,
			Score:      score,
			Reason:     reasons[contentID],
			Algorithm:  algorithms[contentID],
			Confidence: confidences[contentID],
			Timestamp:  time.Now(),
			ExpiresAt:  time.Now().Add(24 * time.Hour),
		})
	}

	return recommendations
}

// AddTrendingContent adds trending content with popularity score
func (hr *HybridRecommender) AddTrendingContent(contentID string, score float64) error {
	if contentID == "" || score < 0 || score > 1 {
		return fmt.Errorf("invalid content ID or score")
	}

	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.trendingContent[contentID] = score
	hr.lastUpdate = time.Now()

	return nil
}

// GetTrendingRecommendations returns trending content recommendations
func (hr *HybridRecommender) GetTrendingRecommendations(count int) []*Recommendation {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	recommendations := make([]*Recommendation, 0)

	for contentID, score := range hr.trendingContent {
		recommendations = append(recommendations, &Recommendation{
			ContentID:  contentID,
			Score:      score,
			Reason:     "Trending now",
			Algorithm:  "trending",
			Confidence: 0.8,
			Timestamp:  time.Now(),
			ExpiresAt:  time.Now().Add(6 * time.Hour),
		})
	}

	// Sort by score
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	// Limit results
	if len(recommendations) > count {
		recommendations = recommendations[:count]
	}

	return recommendations
}

// AddPersonalizedScore adds personalized score for user-content pair
func (hr *HybridRecommender) AddPersonalizedScore(userID, contentID string, score float64) error {
	if userID == "" || contentID == "" || score < 0 || score > 1 {
		return fmt.Errorf("invalid parameters")
	}

	hr.mu.Lock()
	defer hr.mu.Unlock()

	if _, exists := hr.personalizedScores[userID]; !exists {
		hr.personalizedScores[userID] = make(map[string]float64)
	}

	hr.personalizedScores[userID][contentID] = score
	hr.lastUpdate = time.Now()

	return nil
}

// GetPersonalizedRecommendations returns personalized recommendations for a user
func (hr *HybridRecommender) GetPersonalizedRecommendations(userID string, count int) []*Recommendation {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	userScores, exists := hr.personalizedScores[userID]
	if !exists {
		return nil
	}

	recommendations := make([]*Recommendation, 0)

	for contentID, score := range userScores {
		recommendations = append(recommendations, &Recommendation{
			ContentID:  contentID,
			Score:      score,
			Reason:     "Personalized for you",
			Algorithm:  "personalized",
			Confidence: 0.9,
			Timestamp:  time.Now(),
			ExpiresAt:  time.Now().Add(24 * time.Hour),
		})
	}

	// Sort by score
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	// Limit results
	if len(recommendations) > count {
		recommendations = recommendations[:count]
	}

	return recommendations
}

// SetWeights sets algorithm weights for hybrid recommendation
func (hr *HybridRecommender) SetWeights(
	collaborativeWeight float64,
	contentBasedWeight float64,
	trendingWeight float64,
	personalizedWeight float64,
) error {
	if collaborativeWeight < 0 || contentBasedWeight < 0 || trendingWeight < 0 || personalizedWeight < 0 {
		return fmt.Errorf("weights must be non-negative")
	}

	totalWeight := collaborativeWeight + contentBasedWeight + trendingWeight + personalizedWeight
	if totalWeight == 0 {
		return fmt.Errorf("at least one weight must be positive")
	}

	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.collaborativeWeight = collaborativeWeight
	hr.contentBasedWeight = contentBasedWeight
	hr.trendingWeight = trendingWeight
	hr.personalizedWeight = personalizedWeight

	return nil
}

// SetDiversityFactor sets the diversity factor for recommendations
func (hr *HybridRecommender) SetDiversityFactor(factor float64) error {
	if factor < 0 || factor > 1 {
		return fmt.Errorf("diversity factor must be between 0 and 1")
	}

	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.diversityFactor = factor
	return nil
}

// ApplyDiversityFilter applies diversity filtering to recommendations
func (hr *HybridRecommender) ApplyDiversityFilter(
	recommendations []*Recommendation,
	categoryMap map[string]string,
) []*Recommendation {
	if len(recommendations) == 0 || hr.diversityFactor == 0 {
		return recommendations
	}

	hr.mu.RLock()
	defer hr.mu.RUnlock()

	// Track selected categories
	selectedCategories := make(map[string]int)
	filtered := make([]*Recommendation, 0)

	for _, rec := range recommendations {
		category, exists := categoryMap[rec.ContentID]
		if !exists {
			filtered = append(filtered, rec)
			continue
		}

		// Calculate category diversity penalty
		categoryCount := selectedCategories[category]
		diversityPenalty := float64(categoryCount) * hr.diversityFactor

		// Adjust score based on diversity
		adjustedScore := rec.Score * (1 - diversityPenalty)

		if adjustedScore > 0 {
			rec.Score = adjustedScore
			filtered = append(filtered, rec)
			selectedCategories[category]++
		}
	}

	return filtered
}

// GetWeights returns current algorithm weights
func (hr *HybridRecommender) GetWeights() map[string]float64 {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	return map[string]float64{
		"collaborative": hr.collaborativeWeight,
		"content_based": hr.contentBasedWeight,
		"trending":      hr.trendingWeight,
		"personalized":  hr.personalizedWeight,
		"diversity":     hr.diversityFactor,
	}
}

// GetStats returns hybrid recommender statistics
func (hr *HybridRecommender) GetStats() map[string]interface{} {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	return map[string]interface{}{
		"trending_content":    len(hr.trendingContent),
		"personalized_scores": len(hr.personalizedScores),
		"weights":             hr.GetWeights(),
		"last_update":         hr.lastUpdate,
	}
}

// ClearTrendingContent clears trending content
func (hr *HybridRecommender) ClearTrendingContent() {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.trendingContent = make(map[string]float64)
}

// ClearPersonalizedScores clears personalized scores for a user
func (hr *HybridRecommender) ClearPersonalizedScores(userID string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	delete(hr.personalizedScores, userID)
}
