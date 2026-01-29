package api

// AuthPlugin handles authentication
type AuthPlugin struct{}

// Authenticate authenticates a user
func (p *AuthPlugin) Authenticate(username, password string) (string, error) {
return "token", nil
}
