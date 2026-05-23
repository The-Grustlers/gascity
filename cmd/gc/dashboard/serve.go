package dashboard

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// Serve starts the dashboard HTTP server. The dashboard serves the static SPA
// and proxies supervisor API calls so browsers can stay on the dashboard origin.
func Serve(port int, supervisorURL string) error {
	supervisorURL = strings.TrimRight(strings.TrimSpace(supervisorURL), "/")
	if supervisorURL == "" {
		return fmt.Errorf("dashboard: supervisor URL is empty; pass --api")
	}

	handler, err := NewStaticHandler(supervisorURL)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", port)
	log.Printf("dashboard: listening on http://localhost%s (supervisor=%s)", addr, supervisorURL)
	return http.ListenAndServe(addr, logRequest(handler))
}
