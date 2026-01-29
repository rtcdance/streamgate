package metadata

// Indexer handles metadata indexing
type Indexer struct{}

// Index indexes metadata
func (i *Indexer) Index(id string, data map[string]interface{}) error {
	return nil
}

// Search searches indexed metadata
func (i *Indexer) Search(query string) ([]interface{}, error) {
	return []interface{}{}, nil
}
