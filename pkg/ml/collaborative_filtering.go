package ml

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// CollaborativeFilter implements user-based collaborative filtering
type CollaborativeFilter struct {
	mu              sync.RWMutex
	userSimilarity  map[string]map[string]float64
	userRatings     map[string]map[string]float64
	similarityCache map[string][]*SimilarUser
	lastUpdate      time.Time
	cacheExpiry     time.Duration
}

// SimilarUser represents a similar user with similarity score
type SimilarUser struct {
	UserID       string
	Similarity   float64
	CommonItems  int
	AverageRating float64
}

// NewCollaborativeFilter creates a new collaborative filter
func NewCollaborativeFilter() *CollaborativeFilter {
	return &CollaborativeFilter{
		userSimilarity:  make(map[string]map[string]float64),
		userRatings:     make(map[string]map[string]float64),
		similarityCache: make(map[string][]*SimilarUser),
		cacheExpiry:     10 * time.Minute,
	}
}

// AddRating adds a user rating for content
func (cf *CollaborativeFilter) AddRating(userID, contentID string, rating float64) error {
	if userID == "" || contentID == "" {
		return fmt.Errorf("invalid user or content ID")
	}

	if rating < 0 || rating > 5 {
		return fmt.Errorf("rating must be between 0 and 5")
	}

	cf.mu.Lock()
	defer cf.mu.Unlock()

	if _, exists := cf.userRatings[userID]; !exists {
		cf.userRatings[userID] = make(map[string]float64)
	}

	cf.userRatings[userID][contentID] = rating

	// Invalidate similarity cache
	delete(cf.similarityCache, userID)

	return nil
}

// GetRecommendations returns recommendations using collaborative filtering
func (cf *CollaborativeFilter) GetRecommendations(userID string, count int) []*Recommendation {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	userRatings, exists := cf.userRatings[userID]
	if !exists || len(userRatings) == 0 {
		return nil
	}

	// Find similar users
	similarUsers := cf.findSimilarUsers(userID, 10)
	if len(similarUsers) == 0 {
		return nil
	}

	// Get recommendations from similar users
	recommendations := make(map[string]float64)
	weights := make(map[string]float64)

	for _, similarUser := range similarUsers {
		similarUserRatings := cf.userRatings[similarUser.UserID]

		for contentID, rating := range similarUserRatings {
			// Skip items already rated by user
			if _, rated := userRatings[contentID]; rated {
				continue
			}

			// Weight the rating by similarity
			weightedRating := rating * similarUser.Similarity
			recommendations[contentID] += weightedRating
			weights[contentID] += similarUser.Similarity
		}
	}

	// Normalize recommendations
	recs := make([]*Recommendation, 0)
	for contentID, totalScore := range recommendations {
		if weights[contentID] > 0 {
			score := totalScore / weights[contentID]
			confidence := math.Min(weights[contentID]/float64(len(similarUsers)), 1.0)

			recs = append(recs, &Recommendation{
				ContentID:  contentID,
				Score:      score,
				Reason:     "Similar users liked this",
				Algorithm:  "collaborative_filtering",
				Confidence: confidence,
				Timestamp:  time.Now(),
				ExpiresAt:  time.Now().Add(24 * time.Hour),
			})
		}
	}

	// Sort by score
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Score > recs[j].Score
	})

	// Limit results
	if len(recs) > count {
		recs = recs[:count]
	}

	return recs
}

// findSimilarUsers finds similar users using cosine similarity
func (cf *CollaborativeFilter) findSimilarUsers(userID string, limit int) []*SimilarUser {
	userRatings, exists := cf.userRatings[userID]
	if !exists || len(userRatings) == 0 {
		return nil
	}

	// Check cache
	if cached, exists := cf.similarityCache[userID]; exists {
		if time.Since(cf.lastUpdate) < cf.cacheExpiry {
			return cached
		}
	}

	similarities := make([]*SimilarUser, 0)

	for otherUserID, otherRatings := range cf.userRatings {
		if otherUserID == userID {
			continue
		}

		// Find common items
		commonItems := 0
		dotProduct := 0.0
		userMagnitude := 0.0
		otherMagnitude := 0.0

		for contentID, rating := range userRatings {
			if otherRating, exists := otherRatings[contentID]; exists {
				commonItems++
				dotProduct += rating * otherRating
			}
			userMagnitude += rating * rating
		}

		for _, rating := range otherRatings {
			otherMagnitude += rating * rating
		}

		// Calculate cosine similarity
		if commonItems > 0 && userMagnitude > 0 && otherMagnitude > 0 {
			similarity := dotProduct / (math.Sqrt(userMagnitude) * math.Sqrt(otherMagnitude))

			// Calculate average rating of other user
			avgRating := 0.0
			if len(otherRatings) > 0 {
				sum := 0.0
				for _, rating := range otherRatings {
					sum += rating
				}
				avgRating = sum / float64(len(otherRatings))
			}

			similarities = append(similarities, &SimilarUser{
				UserID:        otherUserID,
				Similarity:    similarity,
				CommonItems:   commonItems,
				AverageRating: avgRating,
			})
		}
	}

	// Sort by similarity
	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].Similarity > similarities[j].Similarity
	})

	// Limit results
	if len(similarities) > limit {
		similarities = similarities[:limit]
	}

	// Cache results
	cf.similarityCache[userID] = similarities
	cf.lastUpdate = time.Now()

	return similarities
}

// GetUserSimilarity returns similarity between two users
func (cf *CollaborativeFilter) GetUserSimilarity(userID1, userID2 string) (float64, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	ratings1, exists1 := cf.userRatings[userID1]
	ratings2, exists2 := cf.userRatings[userID2]

	if !exists1 || !exists2 {
		return 0, fmt.Errorf("user not found")
	}

	if len(ratings1) == 0 || len(ratings2) == 0 {
		return 0, fmt.Errorf("insufficient ratings")
	}

	// Calculate cosine similarity
	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for contentID, rating1 := range ratings1 {
		if rating2, exists := ratings2[contentID]; exists {
			dotProduct += rating1 * rating2
		}
		magnitude1 += rating1 * rating1
	}

	for _, rating := range ratings2 {
		magnitude2 += rating * rating
	}

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0, fmt.Errorf("invalid ratings")
	}

	similarity := dotProduct / (math.Sqrt(magnitude1) * math.Sqrt(magnitude2))
	return similarity, nil
}

// GetItemSimilarity returns similarity between two items based on user ratings
func (cf *CollaborativeFilter) GetItemSimilarity(itemID1, itemID2 string) (float64, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	// Collect ratings for both items
	ratings1 := make([]float64, 0)
	ratings2 := make([]float64, 0)

	for _, userRatings := range cf.userRatings {
		if r1, exists1 := userRatings[itemID1]; exists1 {
			if r2, exists2 := userRatings[itemID2]; exists2 {
				ratings1 = append(ratings1, r1)
				ratings2 = append(ratings2, r2)
			}
		}
	}

	if len(ratings1) == 0 {
		return 0, fmt.Errorf("insufficient common ratings")
	}

	// Calculate cosine similarity
	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := range ratings1 {
		dotProduct += ratings1[i] * ratings2[i]
		magnitude1 += ratings1[i] * ratings1[i]
		magnitude2 += ratings2[i] * ratings2[i]
	}

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0, fmt.Errorf("invalid ratings")
	}

	similarity := dotProduct / (math.Sqrt(magnitude1) * math.Sqrt(magnitude2))
	return similarity, nil
}

// GetStats returns collaborative filter statistics
func (cf *CollaborativeFilter) GetStats() map[string]interface{} {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	totalRatings := 0
	for _, userRatings := range cf.userRatings {
		totalRatings += len(userRatings)
	}

	return map[string]interface{}{
		"total_users":      len(cf.userRatings),
		"total_ratings":    totalRatings,
		"cache_size":       len(cf.similarityCache),
		"last_update":      cf.lastUpdate,
	}
}

// ClearCache clears the similarity cache
func (cf *CollaborativeFilter) ClearCache() {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	cf.similarityCache = make(map[string][]*SimilarUser)
}
