package transcoding

func taskSnapshot(task *TranscodingTask) *TranscodingTask {
	snapshot := *task
	snapshot.Metadata = make(map[string]interface{}, len(task.Metadata))
	for key, value := range task.Metadata {
		snapshot.Metadata[key] = metadataSnapshot(value)
	}
	if task.StartedAt != nil {
		started := *task.StartedAt
		snapshot.StartedAt = &started
	}
	if task.CompletedAt != nil {
		completed := *task.CompletedAt
		snapshot.CompletedAt = &completed
	}
	return &snapshot
}

func metadataSnapshot(value interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		if value == nil {
			return nil
		}
		snapshot := make(map[string]interface{}, len(value))
		for key, entry := range value {
			snapshot[key] = metadataSnapshot(entry)
		}
		return snapshot
	case []interface{}:
		if value == nil {
			return nil
		}
		snapshot := make([]interface{}, len(value))
		for index, entry := range value {
			snapshot[index] = metadataSnapshot(entry)
		}
		return snapshot
	default:
		return value
	}
}
