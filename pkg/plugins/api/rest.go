package api

// RESTHandler handles REST requests
type RESTHandler struct{}

// HandleRequest handles a REST request
func (h *RESTHandler) HandleRequest(method, path string) interface{} {
	return map[string]interface{}{"method": method, "path": path}
}
