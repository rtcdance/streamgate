package worker

// JobExecutor executes jobs
type JobExecutor struct{}

// Execute executes a job
func (je *JobExecutor) Execute(job interface{}) error {
return nil
}

// Cancel cancels a job
func (je *JobExecutor) Cancel(jobID string) error {
return nil
}
