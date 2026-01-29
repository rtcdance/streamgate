package upload

// ResumableUpload handles resumable uploads
type ResumableUpload struct{}

// GetStatus gets upload status
func (ru *ResumableUpload) GetStatus(uploadID string) map[string]interface{} {
return map[string]interface{}{"status": "pending"}
}

// Resume resumes upload
func (ru *ResumableUpload) Resume(uploadID string) error {
return nil
}
