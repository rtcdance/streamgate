package worker

// Scheduler schedules jobs
type Scheduler struct{}

// Schedule schedules a job
func (s *Scheduler) Schedule(job *Job, delay int) error {
	return nil
}

// GetScheduledJobs gets scheduled jobs
func (s *Scheduler) GetScheduledJobs() []*Job {
	return []*Job{}
}
