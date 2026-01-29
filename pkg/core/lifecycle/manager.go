package lifecycle

// Manager manages lifecycle
type Manager struct {
	lifecycle *Lifecycle
}

// NewManager creates a new lifecycle manager
func NewManager() *Manager {
	return &Manager{
		lifecycle: NewLifecycle(),
	}
}

// GetLifecycle returns the lifecycle
func (m *Manager) GetLifecycle() *Lifecycle {
	return m.lifecycle
}
