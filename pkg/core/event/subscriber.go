package event

// Subscriber subscribes to events
type Subscriber struct {
	eventType string
	handler   func(Event)
}

// NewSubscriber creates a new subscriber
func NewSubscriber(eventType string, handler func(Event)) *Subscriber {
	return &Subscriber{
		eventType: eventType,
		handler:   handler,
	}
}

// GetEventType returns the event type
func (s *Subscriber) GetEventType() string {
	return s.eventType
}

// Handle handles an event
func (s *Subscriber) Handle(event Event) {
	if event.Type == s.eventType {
		s.handler(event)
	}
}
