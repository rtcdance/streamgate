package ml

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ContentBasedFilter implements content-based filtering
type ContentBasedFilter struct {
	mu              sync.RWMutex
	contentFeatures map[string][]float64
	userPreferences map[string][]float64
	categoryWeights map[string]float64
	tagWeights      map[string]float64
	lastUpdate      time.Time
}

// NewContentBasedFilter creates a new content-based filter
func NewContentBasedFilter() *ContentBasedFilter {
	return &ContentBasedFilter{
		contentFeatures: make(map[string][]float64),
		userPreferences: make(map[string][]float64),
		categoryWeights: make(map[string]float64),
		tagWeights:      make(map[string]float64),
	}
}

// AddContentFeatures adds feature vector for content
func (cbf *ContentBasedFilter) AddContentFeatures(contentID string, features []float64) error {
	if contentID == "" || len(features) == 0 {
		return fmt.Errorf("invalid content ID or features")
	}

	cbf.mu.Lock()
	defer cbf.mu.Unlock()

	cbf.contentFeatures[contentID] = features
	return nil
}

// UpdateUserPreferences updates user preference vector based on viewed content
func (cbf *ContentBasedFilter) UpdateUserPreferences(userProfile *UserProfile) error {
	if userProfile == nil || userProfile.UserID == "" {
		return fmt.Errorf("invalid user profile")
	}

	cbf.mu.Lock()
	defer cbf.mu.Unlock()

	if len(userProfile.ViewedContent) == 0 {
		return fmt.Errorf("user has no viewed content")
	}

	// Calculate preference vector as weighted average of viewed content features
	preferenceVector := make([]float64, 0)
	totalWeight := 0.0

	for _, contentID := range userProfile.ViewedContent {
		if features, exists := cbf.contentFeatures[contentID]; exists {
			weight := 1.0
			if rating, exists := userProfile.Ratings[contentID]; exists {
				weight = rating / 5.0 // Normalize rating to 0-1
			}

			if len(preferenceVector) == 0 {
				preferenceVector = make([]float64, len(features))
			}

			for i, feature := range features {
				preferenceVector[i] += feature * weight
			}
			totalWeight += weight
		}
	}

	if totalWeight > 0 && len(preferenceVector) > 0 {
		for i := range preferenceVector {
			preferenceVector[i] /= totalWeight
		}
		cbf.userPreferences[userProfile.UserID] = preferenceVector
	}

	return nil
}

// GetRecommendations returns recommendations using content-based filtering
func (cbf *ContentBasedFilter) GetRecommendations(userProfile *UserProfile, count int) []*Recommendation {
	cbf.mu.RLock()
	defer cbf.mu.RUnlock()

	preferences, exists := cbf.userPreferences[userProfile.UserID]
	if !exists || len(preferences) == 0 {
		return nil
	}

	viewedSet := make(map[string]bool)
	for _, contentID := range userProfile.ViewedContent {
		viewedSet[contentID] = true
	}

	recommendations := make([]*Recommendation, 0)

	// Calculate similarity between user preferences and content features
	for contentID, features := range cbf.contentFeatures {
		// Skip already viewed content
		if viewedSet[contentID] {
			continue
		}

		// Calculate cosine similarity
		similarity := cbf.cosineSimilarity(preferences, features)

		// Calculate confidence based on feature vector quality
		confidence := cbf.calculateConfidence(features)

		recommendations = append(recommendations, &Recommendation{
			ContentID:  contentID,
			Score:      similarity,
			Reason:     "Similar to content you liked",
			Algorithm:  "content_based",
			Confidence: confidence,
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

// cosineSimilarity calculates cosine similarity between two vectors
func (cbf *ContentBasedFilter) cosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) || len(vec1) == 0 {
		return 0
	}

	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := range vec1 {
		dotProduct += vec1[i] * vec2[i]
		magnitude1 += vec1[i] * vec1[i]
		magnitude2 += vec2[i] * vec2[i]
	}

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(magnitude1) * math.Sqrt(magnitude2))
}

// calculateConfidence calculates confidence score for a feature vector
func (cbf *ContentBasedFilter) calculateConfidence(features []float64) float64 {
	if len(features) == 0 {
		return 0
	}

	// Confidence based on feature magnitude
	magnitude := 0.0
	for _, f := range features {
		magnitude += f * f
	}
	magnitude = math.Sqrt(magnitude)

	// Normalize to 0-1 range
	confidence := math.Min(magnitude, 1.0)
	return confidence
}

// UpdateCategoryWeights updates weights for content categories
func (cbf *ContentBasedFilter) UpdateCategoryWeights(categoryWeights map[string]float64) error {
	if len(categoryWeights) == 0 {
		return fmt.Errorf("invalid category weights")
	}

	cbf.mu.Lock()
	defer cbf.mu.Unlock()

	cbf.categoryWeights = categoryWeights
	cbf.lastUpdate = time.Now()

	return nil
}

// UpdateTagWeights updates weights for content tags
func (cbf *ContentBasedFilter) UpdateTagWeights(tagWeights map[string]float64) error {
	if len(tagWeights) == 0 {
		return fmt.Errorf("invalid tag weights")
	}

	cbf.mu.Lock()
	defer cbf.mu.Unlock()

	cbf.tagWeights = tagWeights
	cbf.lastUpdate = time.Now()

	return nil
}

// GetContentSimilarity returns similarity between two content items
func (cbf *ContentBasedFilter) GetContentSimilarity(contentID1, contentID2 string) (float64, error) {
	cbf.mu.RLock()
	defer cbf.mu.RUnlock()

	features1, exists1 := cbf.contentFeatures[contentID1]
	features2, exists2 := cbf.contentFeatures[contentID2]

	if !exists1 || !exists2 {
		return 0, fmt.Errorf("content not found")
	}

	if len(features1) == 0 || len(features2) == 0 {
		return 0, fmt.Errorf("invalid features")
	}

	similarity := cbf.cosineSimilarity(features1, features2)
	return similarity, nil
}

// GetUserPreferences returns user preference vector
func (cbf *ContentBasedFilter) GetUserPreferences(userID string) ([]float64, error) {
	cbf.mu.RLock()
	defer cbf.mu.RUnlock()

	preferences, exists := cbf.userPreferences[userID]
	if !exists {
		return nil, fmt.Errorf("user preferences not found")
	}

	return preferences, nil
}

// GetStats returns content-based filter statistics
func (cbf *ContentBasedFilter) GetStats() map[string]interface{} {
	cbf.mu.RLock()
	defer cbf.mu.RUnlock()

	return map[string]interface{}{
		"total_content":    len(cbf.contentFeatures),
		"total_users":      len(cbf.userPreferences),
		"category_weights": len(cbf.categoryWeights),
		"tag_weights":      len(cbf.tagWeights),
		"last_update":      cbf.lastUpdate,
	}
}

// ClearUserPreferences clears preferences for a user
func (cbf *ContentBasedFilter) ClearUserPreferences(userID string) {
	cbf.mu.Lock()
	defer cbf.mu.Unlock()

	delete(cbf.userPreferences, userID)
}

// ClearAllPreferences clears all user preferences
func (cbf *ContentBasedFilter) ClearAllPreferences() {
	cbf.mu.Lock()
	defer cbf.mu.Unlock()

	cbf.userPreferences = make(map[string][]float64)
}
