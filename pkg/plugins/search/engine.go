package search

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SearchEngine provides full-text search capabilities
type SearchEngine struct {
	index      *InvertedIndex
	logger     *zap.Logger
	mu         sync.RWMutex
	stopWords  map[string]bool
	tokenizer  *Tokenizer
	normalizer *Normalizer
}

// InvertedIndex represents an inverted index for full-text search
type InvertedIndex struct {
	documents map[string]*Document
	terms     map[string][]*Posting
	mu        sync.RWMutex
}

// Document represents a searchable document
type Document struct {
	ID        string
	Content   string
	Metadata  map[string]interface{}
	Timestamp time.Time
	Score     float64
}

// Posting represents a posting in the inverted index
type Posting struct {
	DocID    string
	Position int
	Score    float64
}

// NewSearchEngine creates a new search engine
func NewSearchEngine(logger *zap.Logger) *SearchEngine {
	return &SearchEngine{
		index:      NewInvertedIndex(),
		logger:     logger,
		stopWords:  loadStopWords(),
		tokenizer:  NewTokenizer(),
		normalizer: NewNormalizer(),
	}
}

// NewInvertedIndex creates a new inverted index
func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		documents: make(map[string]*Document),
		terms:     make(map[string][]*Posting),
	}
}

// IndexDocument indexes a document for search
func (se *SearchEngine) IndexDocument(ctx context.Context, doc *Document) error {
	se.logger.Debug("Indexing document",
		zap.String("doc_id", doc.ID),
		zap.Int("content_length", len(doc.Content)))

	se.mu.Lock()
	defer se.mu.Unlock()

	// Store document
	se.index.documents[doc.ID] = doc

	// Tokenize and normalize content
	tokens := se.tokenizer.Tokenize(doc.Content)
	normalizedTokens := se.normalizer.Normalize(tokens)

	// Remove stop words
	filteredTokens := se.removeStopWords(normalizedTokens)

	// Index each term
	for pos, token := range filteredTokens {
		if token == "" {
			continue
		}

		posting := &Posting{
			DocID:    doc.ID,
			Position: pos,
			Score:    1.0,
		}

		se.index.terms[token] = append(se.index.terms[token], posting)
	}

	se.logger.Debug("Document indexed",
		zap.String("doc_id", doc.ID),
		zap.Int("terms_indexed", len(filteredTokens)))

	return nil
}

// IndexMetadata indexes metadata for search
func (se *SearchEngine) IndexMetadata(ctx context.Context, id string, metadata map[string]interface{}) error {
	se.logger.Debug("Indexing metadata",
		zap.String("id", id),
		zap.Int("fields", len(metadata)))

	// Convert metadata to searchable text
	content := se.metadataToText(metadata)

	doc := &Document{
		ID:       id,
		Content:  content,
		Metadata: metadata,
	}

	return se.IndexDocument(ctx, doc)
}

// Search performs a full-text search
func (se *SearchEngine) Search(ctx context.Context, query string, opts *SearchOptions) (*SearchResult, error) {
	se.logger.Debug("Searching",
		zap.String("query", query),
		zap.Int("limit", opts.Limit),
		zap.Int("offset", opts.Offset))

	se.mu.RLock()
	defer se.mu.RUnlock()

	// Tokenize and normalize query
	tokens := se.tokenizer.Tokenize(query)
	normalizedTokens := se.normalizer.Normalize(tokens)
	filteredTokens := se.removeStopWords(normalizedTokens)

	if len(filteredTokens) == 0 {
		return &SearchResult{
			Query:   query,
			Total:   0,
			Results: []*Document{},
		}, nil
	}

	// Find matching documents
	docScores := make(map[string]float64)

	for _, token := range filteredTokens {
		postings, exists := se.index.terms[token]
		if !exists {
			continue
		}

		for _, posting := range postings {
			docScores[posting.DocID] += posting.Score
		}
	}

	// Sort by score
	sortedDocs := make([]*Document, 0, len(docScores))
	for docID, score := range docScores {
		doc, exists := se.index.documents[docID]
		if !exists {
			continue
		}

		doc.Score = score
		sortedDocs = append(sortedDocs, doc)
	}

	sort.Slice(sortedDocs, func(i, j int) bool {
		return sortedDocs[i].Score > sortedDocs[j].Score
	})

	// Apply pagination
	total := len(sortedDocs)
	start := opts.Offset
	end := start + opts.Limit

	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	paginatedResults := sortedDocs[start:end]

	// Highlight matches
	if opts.Highlight {
		for _, doc := range paginatedResults {
			doc.Content = se.highlightMatches(doc.Content, filteredTokens)
		}
	}

	se.logger.Debug("Search completed",
		zap.String("query", query),
		zap.Int("total", total),
		zap.Int("returned", len(paginatedResults)))

	return &SearchResult{
		Query:   query,
		Total:   total,
		Results: paginatedResults,
		Took:    time.Since(time.Now()),
	}, nil
}

// DeleteDocument removes a document from the index
func (se *SearchEngine) DeleteDocument(ctx context.Context, docID string) error {
	se.logger.Debug("Deleting document",
		zap.String("doc_id", docID))

	se.mu.Lock()
	defer se.mu.Unlock()

	// Remove document
	delete(se.index.documents, docID)

	// Remove postings
	for term, postings := range se.index.terms {
		filtered := make([]*Posting, 0)
		for _, posting := range postings {
			if posting.DocID != docID {
				filtered = append(filtered, posting)
			}
		}
		if len(filtered) == 0 {
			delete(se.index.terms, term)
		} else {
			se.index.terms[term] = filtered
		}
	}

	return nil
}

// UpdateDocument updates a document in the index
func (se *SearchEngine) UpdateDocument(ctx context.Context, doc *Document) error {
	se.logger.Debug("Updating document",
		zap.String("doc_id", doc.ID))

	if err := se.DeleteDocument(ctx, doc.ID); err != nil {
		return err
	}

	return se.IndexDocument(ctx, doc)
}

// GetDocument retrieves a document from the index
func (se *SearchEngine) GetDocument(ctx context.Context, docID string) (*Document, error) {
	se.mu.RLock()
	defer se.mu.RUnlock()

	doc, exists := se.index.documents[docID]
	if !exists {
		return nil, fmt.Errorf("document not found: %s", docID)
	}

	return doc, nil
}

// GetStats returns index statistics
func (se *SearchEngine) GetStats(ctx context.Context) *IndexStats {
	se.mu.RLock()
	defer se.mu.RUnlock()

	return &IndexStats{
		TotalDocuments: len(se.index.documents),
		TotalTerms:     len(se.index.terms),
	}
}

// metadataToText converts metadata to searchable text
func (se *SearchEngine) metadataToText(metadata map[string]interface{}) string {
	var parts []string

	for key, value := range metadata {
		strValue := fmt.Sprintf("%v", value)
		parts = append(parts, fmt.Sprintf("%s:%s", key, strValue))
	}

	return strings.Join(parts, " ")
}

// removeStopWords removes stop words from tokens
func (se *SearchEngine) removeStopWords(tokens []string) []string {
	filtered := make([]string, 0, len(tokens))

	for _, token := range tokens {
		if !se.stopWords[token] {
			filtered = append(filtered, token)
		}
	}

	return filtered
}

// highlightMatches highlights matching terms in content
func (se *SearchEngine) highlightMatches(content string, terms []string) string {
	highlighted := content

	for _, term := range terms {
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(term) + `\b`)
		highlighted = pattern.ReplaceAllString(highlighted, `**$0**`)
	}

	return highlighted
}

// SearchOptions represents search options
type SearchOptions struct {
	Limit     int
	Offset    int
	Highlight bool
}

// SearchResult represents search results
type SearchResult struct {
	Query   string
	Total   int
	Results []*Document
	Took    time.Duration
}

// IndexStats represents index statistics
type IndexStats struct {
	TotalDocuments int
	TotalTerms     int
}

// Tokenizer handles tokenization
type Tokenizer struct {
	wordRegex *regexp.Regexp
}

// NewTokenizer creates a new tokenizer
func NewTokenizer() *Tokenizer {
	return &Tokenizer{
		wordRegex: regexp.MustCompile(`\w+`),
	}
}

// Tokenize splits text into tokens
func (t *Tokenizer) Tokenize(text string) []string {
	matches := t.wordRegex.FindAllString(text, -1)
	return matches
}

// Normalizer handles text normalization
type Normalizer struct{}

// NewNormalizer creates a new normalizer
func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

// Normalize normalizes tokens
func (n *Normalizer) Normalize(tokens []string) []string {
	normalized := make([]string, len(tokens))

	for i, token := range tokens {
		normalized[i] = strings.ToLower(token)
	}

	return normalized
}

// loadStopWords loads common stop words
func loadStopWords() map[string]bool {
	return map[string]bool{
		"a":    true,
		"an":   true,
		"and":  true,
		"are":  true,
		"as":   true,
		"at":   true,
		"be":   true,
		"by":   true,
		"for":  true,
		"from": true,
		"has":  true,
		"he":   true,
		"in":   true,
		"is":   true,
		"it":   true,
		"its":  true,
		"of":   true,
		"on":   true,
		"that": true,
		"the":  true,
		"to":   true,
		"was":  true,
		"were": true,
		"will": true,
		"with": true,
	}
}

// AdvancedSearch performs advanced search with filters
func (se *SearchEngine) AdvancedSearch(ctx context.Context, query string, filters map[string]interface{}, opts *SearchOptions) (*SearchResult, error) {
	se.logger.Debug("Performing advanced search",
		zap.String("query", query),
		zap.Int("filters", len(filters)))

	// Perform basic search
	result, err := se.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	// Apply filters
	filteredResults := make([]*Document, 0)
	for _, doc := range result.Results {
		if se.matchesFilters(doc, filters) {
			filteredResults = append(filteredResults, doc)
		}
	}

	result.Results = filteredResults
	result.Total = len(filteredResults)

	return result, nil
}

// matchesFilters checks if a document matches the filters
func (se *SearchEngine) matchesFilters(doc *Document, filters map[string]interface{}) bool {
	for key, value := range filters {
		docValue, exists := doc.Metadata[key]
		if !exists {
			return false
		}

		if !se.compareValues(docValue, value) {
			return false
		}
	}

	return true
}

// compareValues compares two values
func (se *SearchEngine) compareValues(a, b interface{}) bool {
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)

	return aStr == bStr
}

// Suggest provides search suggestions
func (se *SearchEngine) Suggest(ctx context.Context, prefix string, limit int) ([]string, error) {
	se.logger.Debug("Generating suggestions",
		zap.String("prefix", prefix),
		zap.Int("limit", limit))

	se.mu.RLock()
	defer se.mu.RUnlock()

	// Normalize prefix
	normalizedPrefix := strings.ToLower(prefix)

	// Find matching terms
	var suggestions []string
	for term := range se.index.terms {
		if strings.HasPrefix(term, normalizedPrefix) {
			suggestions = append(suggestions, term)
		}
	}

	// Sort suggestions
	sort.Strings(suggestions)

	// Limit results
	if limit > 0 && len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}

	return suggestions, nil
}

// Reindex rebuilds the entire index
func (se *SearchEngine) Reindex(ctx context.Context) error {
	se.logger.Debug("Rebuilding index")

	se.mu.Lock()
	defer se.mu.Unlock()

	// Clear existing index
	se.index = NewInvertedIndex()

	// Re-index all documents
	for _, doc := range se.index.documents {
		if err := se.IndexDocument(ctx, doc); err != nil {
			se.logger.Error("Failed to re-index document",
				zap.String("doc_id", doc.ID),
				zap.Error(err))
		}
	}

	return nil
}

// ExportIndex exports the index to JSON
func (se *SearchEngine) ExportIndex(ctx context.Context) ([]byte, error) {
	se.mu.RLock()
	defer se.mu.RUnlock()

	data := map[string]interface{}{
		"documents": se.index.documents,
		"terms":     se.index.terms,
	}

	return json.MarshalIndent(data, "", "  ")
}

// ImportIndex imports the index from JSON
func (se *SearchEngine) ImportIndex(ctx context.Context, data []byte) error {
	var imported struct {
		Documents map[string]*Document
		Terms     map[string][]*Posting
	}

	if err := json.Unmarshal(data, &imported); err != nil {
		return fmt.Errorf("failed to unmarshal index: %w", err)
	}

	se.mu.Lock()
	defer se.mu.Unlock()

	se.index.documents = imported.Documents
	se.index.terms = imported.Terms

	return nil
}
