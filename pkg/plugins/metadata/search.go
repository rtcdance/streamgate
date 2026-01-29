package metadata

// Searcher handles metadata search
type Searcher struct{}

// Search searches metadata
func (s *Searcher) Search(query string) ([]interface{}, error) {
	return []interface{}{}, nil
}

// Filter filters metadata
func (s *Searcher) Filter(criteria map[string]interface{}) ([]interface{}, error) {
	return []interface{}{}, nil
}
