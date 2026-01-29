package metadata

// Database handles metadata database operations
type Database struct{}

// Query queries metadata
func (db *Database) Query(sql string) (interface{}, error) {
return nil, nil
}

// Insert inserts metadata
func (db *Database) Insert(table string, data map[string]interface{}) error {
return nil
}

// Update updates metadata
func (db *Database) Update(table string, id string, data map[string]interface{}) error {
return nil
}

// Delete deletes metadata
func (db *Database) Delete(table string, id string) error {
return nil
}
