package transcoder

// Worker handles transcoding work
type Worker struct {
ID string
}

// Process processes a transcoding task
func (w *Worker) Process(task interface{}) error {
return nil
}

// GetStatus gets worker status
func (w *Worker) GetStatus() string {
return "idle"
}
