package upload

// StorageBackend handles storage operations
type StorageBackend struct{}

// Store stores file
func (sb *StorageBackend) Store(fileID string, data []byte) error {
return nil
}

// Retrieve retrieves file
func (sb *StorageBackend) Retrieve(fileID string) ([]byte, error) {
return []byte{}, nil
}

// Delete deletes file
func (sb *StorageBackend) Delete(fileID string) error {
return nil
}
