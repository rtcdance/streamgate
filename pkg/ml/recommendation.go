package ml

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// RecommendationEngine provides content recommendations using multiple algorithms
type RecommendationEngine struct {
	mu                    sync.RWMutex
	collaborativeFilter   *CollaborativeFilter
	contentBasedFilter    *ContentBasedFilter
	hybridRecommender     *HybridRecommender
	userProfiles          map[string]*UserProfile
	contentProfiles       map[string]*ContentProfile
	recommendations       map[string][]*Recommendation
	metrics               *RecommendationMetrics
	lastUpdateTime        time.Time
	updateInterval        time.Duration
}

// UserProfile represents a user's preferences and behavior
type UserProfile struct {
	UserID           string
	ViewedContent    []string
	Ratings          map[string]float64
	Preferences      map[string]float64
	LastActive       time.Time
	EngagementScore  float64
	DiversityScore   float64
	TrendingAffinity float64
}

// ContentProfile represents content characteristics
type ContentProfile struct {
	ContentID      string
	Title          string
	Category       string
	Tags           []string
	Features       map[string]float64
	Popularity     float64
	ViewCount      int64
	AvgRating      float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Embeddings     []float64
}

// Recommendation represents a recommended content item
type Recommendation struct {
	ContentID      string
	Score          float64
	Reason         string
	Algorithm      string
	Confidence     float64
	Timestamp      time.Time
	ExpiresAt      time.Time
}

// RecommendationMetrics tracks recommendation performance
type RecommendationMetrics struct {
	TotalRecommendations int64
	AcceptedCount        int64
	RejectedCount        int64
	ClickThroughRate     float64
	ConversionRate       float64
	AveragePrecision     float64
	NormalizedDCG        float64
	CoverageRate         float64
	DiversityScore       float64
	LastUpdated          time.Time
}

// NewRecommendationEngine creates a new recommendation engine
func NewRecommendationEngine() *RecommendationEngine {
	return &RecommendationEngine{
		collaborativeFilter: NewCollaborativeFilter(),
		contentBasedFilter:  NewContentBasedFilter(),
		hybridRecommender:   NewHybridRecommender(),
		userProfiles:        make(map[string]*UserProfile),
		contentProfiles:     make(map[string]*ContentProfile),
		recommendations:     make(map[string][]*Recommendation),
		metrics:             &RecommendationMetrics{},
		updateInterval:      5 * time.Minute,
	}
}

// AddUserProfile adds or updates a user profile
func (re *RecommendationEngine) AddUserProfile(profile *UserProfile) error {
	if profile == nil || profile.UserID == "" {
		return fmt.Errorf("invalid user profile")
	}

	re.mu.Lock()
	defer re.mu.Unlock()

	profile.LastActive = time.Now()
	re.userProfiles[profile.UserID] = profile

	return nil
}

// AddContentProfile adds or updates a content profile
func (re *RecommendationEngine) AddContentProfile(profile *ContentProfile) error {
	if profile == nil || profile.ContentID == "" {
		return fmt.Errorf("invalid content profile")
	}

	re.mu.Lock()
	defer re.mu.Unlock()

	profile.UpdatedAt = time.Now()
	re.contentProfiles[profile.ContentID] = profile

	return nil
}

// RecordUserInteraction records user interaction with content
func (re *RecommendationEngine) RecordUserInteraction(userID, contentID string, rating float64) error {
	if userID == "" || contentID == "" {
		return fmt.Errorf("invalid user or content ID")
	}

	if rating < 0 || rating > 5 {
		return fmt.Errorf("rating must be between 0 and 5")
	}

	re.mu.Lock()
	defer re.mu.Unlock()

	// Update user profile
	userProfile, exists := re.userProfiles[userID]
	if !exists {
		userProfile = &UserProfile{
			UserID:      userID,
			ViewedContent: []string{},
			Ratings:     make(map[string]float64),
			Preferences: make(map[string]float64),
		}
		re.userProfiles[userID] = userProfile
	}

	userProfile.ViewedContent = append(userProfile.ViewedContent, contentID)
	userProfile.Ratings[contentID] = rating
	userProfile.LastActive = time.Now()

	// Update content profile
	contentProfile, exists := re.contentProfiles[contentID]
	if exists {
		contentProfile.ViewCount++
		// Update average rating
		totalRating := contentProfile.AvgRating * float64(contentProfile.ViewCount-1)
		contentProfile.AvgRating = (totalRating + rating) / float64(contentProfile.ViewCount)
	}

	return nil
}

// GetRecommendations returns recommendations for a user
func (re *RecommendationEngine) GetRecommendations(ctx context.Context, userID string, count int) ([]*Recommendation, error) {
	if userID == "" || count <= 0 {
		return nil, fmt.Errorf("invalid user ID or count")
	}

	re.mu.RLock()
	userProfile, exists := re.userProfiles[userID]
	re.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("user profile not found")
	}

	// Get recommendations from different algorithms
	collaborativeRecs := re.collaborativeFilter.GetRecommendations(userID, count)
	contentBasedRecs := re.contentBasedFilter.GetRecommendations(userProfile, count)

	// Combine using hybrid approach
	hybridRecs := re.hybridRecommender.CombineRecommendations(
		collaborativeRecs,
		contentBasedRecs,
		0.5, // weight for collaborative
		0.5, // weight for content-based
	)

	// Sort by score and limit
	sort.Slice(hybridRecs, func(i, j int) bool {
		return hybridRecs[i].Score > hybridRecs[j].Score
	})

	if len(hybridRecs) > count {
		hybridRecs = hybridRecs[:count]
	}

	// Cache recommendations
	re.mu.Lock()
	re.recommendations[userID] = hybridRecs
	re.mu.Unlock()

	// Update metrics
	re.metrics.TotalRecommendations += int64(len(hybridRecs))

	return hybridRecs, nil
}

// RecordRecommendationFeedback records user feedback on recommendations
func (re *RecommendationEngine) RecordRecommendationFeedback(userID, contentID string, accepted bool) error {
	if userID == "" || contentID == "" {
		return fmt.Errorf("invalid user or content ID")
	}

	re.mu.Lock()
	defer re.mu.Unlock()

	if accepted {
		re.metrics.AcceptedCount++
	} else {
		re.metrics.RejectedCount++
	}

	// Update CTR
	total := re.metrics.AcceptedCount + re.metrics.RejectedCount
	if total > 0 {
		re.metrics.ClickThroughRate = float64(re.metrics.AcceptedCount) / float64(total)
	}

	return nil
}

// GetMetrics returns recommendation metrics
func (re *RecommendationEngine) GetMetrics() *RecommendationMetrics {
	re.mu.RLock()
	defer re.mu.RUnlock()

	metrics := *re.metrics
	metrics.LastUpdated = time.Now()
	return &metrics
}

// UpdateMetrics updates recommendation metrics
func (re *RecommendationEngine) UpdateMetrics(ctx context.Context) error {
	re.mu.Lock()
	defer re.mu.Unlock()

	// Calculate coverage rate
	if len(re.contentProfiles) > 0 {
		recommendedContent := make(map[string]bool)
		for _, recs := range re.recommendations {
			for _, rec := range recs {
				recommendedContent[rec.ContentID] = true
			}
		}
		re.metrics.CoverageRate = float64(len(recommendedContent)) / float64(len(re.contentProfiles))
	}

	// Calculate diversity score
	re.metrics.DiversityScore = re.calculateDiversityScore()

	// Calculate average precision
	re.metrics.AveragePrecision = re.calculateAveragePrecision()

	// Calculate normalized DCG
	re.metrics.NormalizedDCG = re.calculateNormalizedDCG()

	re.metrics.LastUpdated = time.Now()
	return nil
}

// calculateDiversityScore calculates the diversity of recommendations
func (re *RecommendationEngine) calculateDiversityScore() float64 {
	if len(re.recommendations) == 0 {
		return 0
	}

	totalDiversity := 0.0
	count := 0

	for _, recs := range re.recommendations {
		if len(recs) > 1 {
			// Calculate category diversity
			categories := make(map[string]int)
			for _, rec := range recs {
				if profile, exists := re.contentProfiles[rec.ContentID]; exists {
					categories[profile.Category]++
				}
			}

			// Diversity = 1 - (max_category_count / total_count)
			maxCount := 0
			for _, count := range categories {
				if count > maxCount {
					maxCount = count
				}
			}

			diversity := 1.0 - float64(maxCount)/float64(len(recs))
			totalDiversity += diversity
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return totalDiversity / float64(count)
}

// calculateAveragePrecision calculates average precision
func (re *RecommendationEngine) calculateAveragePrecision() float64 {
	if re.metrics.TotalRecommendations == 0 {
		return 0
	}

	return float64(re.metrics.AcceptedCount) / float64(re.metrics.TotalRecommendations)
}

// calculateNormalizedDCG calculates normalized discounted cumulative gain
func (re *RecommendationEngine) calculateNormalizedDCG() float64 {
	if len(re.recommendations) == 0 {
		return 0
	}

	totalNDCG := 0.0
	count := 0

	for _, recs := range re.recommendations {
		if len(recs) > 0 {
			// Calculate DCG
			dcg := 0.0
			for i, rec := range recs {
				// Relevance score based on confidence
				relevance := rec.Confidence
				dcg += relevance / float64(i+2) // i+2 because log2(i+1) for i starting at 0
			}

			// Ideal DCG (all items with confidence 1.0)
			idcg := 0.0
			for i := 0; i < len(recs); i++ {
				idcg += 1.0 / float64(i+2)
			}

			if idcg > 0 {
				ndcg := dcg / idcg
				totalNDCG += ndcg
				count++
			}
		}
	}

	if count == 0 {
		return 0
	}

	return totalNDCG / float64(count)
}

// GetUserProfile returns a user profile
func (re *RecommendationEngine) GetUserProfile(userID string) (*UserProfile, error) {
	re.mu.RLock()
	defer re.mu.RUnlock()

	profile, exists := re.userProfiles[userID]
	if !exists {
		return nil, fmt.Errorf("user profile not found")
	}

	return profile, nil
}

// GetContentProfile returns a content profile
func (re *RecommendationEngine) GetContentProfile(contentID string) (*ContentProfile, error) {
	re.mu.RLock()
	defer re.mu.RUnlock()

	profile, exists := re.contentProfiles[contentID]
	if !exists {
		return nil, fmt.Errorf("content profile not found")
	}

	return profile, nil
}

// GetCachedRecommendations returns cached recommendations for a user
func (re *RecommendationEngine) GetCachedRecommendations(userID string) []*Recommendation {
	re.mu.RLock()
	defer re.mu.RUnlock()

	recs, exists := re.recommendations[userID]
	if !exists {
		return nil
	}

	return recs
}

// ClearExpiredRecommendations removes expired recommendations
func (re *RecommendationEngine) ClearExpiredRecommendations() int {
	re.mu.Lock()
	defer re.mu.Unlock()

	now := time.Now()
	cleared := 0

	for userID, recs := range re.recommendations {
		validRecs := make([]*Recommendation, 0)
		for _, rec := range recs {
			if rec.ExpiresAt.After(now) {
				validRecs = append(validRecs, rec)
			} else {
				cleared++
			}
		}
		re.recommendations[userID] = validRecs
	}

	return cleared
}

// GetStats returns recommendation engine statistics
func (re *RecommendationEngine) GetStats() map[string]interface{} {
	re.mu.RLock()
	defer re.mu.RUnlock()

	return map[string]interface{}{
		"total_users":           len(re.userProfiles),
		"total_content":         len(re.contentProfiles),
		"cached_recommendations": len(re.recommendations),
		"metrics":               re.metrics,
	}
}
