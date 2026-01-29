package cache

// Redis represents Redis cache
type Redis struct {
	host string
	port int
}

// NewRedis creates a new Redis cache
func NewRedis(host string, port int) *Redis {
	return &Redis{
		host: host,
		port: port,
	}
}

// Connect connects to Redis
func (r *Redis) Connect() error {
	// TODO: Implement Redis connection
	return nil
}
