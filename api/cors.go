package api

import "net/http"

// withCORS allows exactly one configured origin. A request from any other
// origin gets no CORS headers, so the browser refuses to expose the response.
func (s *Server) withCORS(next http.Handler) http.Handler {
	origin := s.cfg.CORSOrigin
	if origin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == origin {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
			h.Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
