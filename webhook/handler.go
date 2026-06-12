package webhook

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/pboyd-oss/platform-attest-coordinator/coordinator"
	"github.com/pboyd-oss/platform-attest-coordinator/ui"
)

type Handler struct {
	coord  *coordinator.Coordinator
	ui     *ui.Handler
	secret string
	log    *log.Logger
}

func NewHandler(coord *coordinator.Coordinator, secret string, logger *log.Logger) *Handler {
	return &Handler{
		coord:  coord,
		ui:     ui.NewHandler(coord),
		secret: secret,
		log:    logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/webhook/build-event":
		h.handleBuildEvent(w, r)
	case r.URL.Path == "/" || r.URL.Path == "/ui" || r.URL.Path == "/suite.css" ||
		r.URL.Path == "/api/builds" || strings.HasPrefix(r.URL.Path, "/api/builds/"):
		h.ui.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleBuildEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.secret != "" && r.Header.Get("Authorization") != "Bearer "+h.secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload coordinator.BuildEventPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	jobType := coordinator.ClassifyJob(payload.JobPath)
	h.log.Printf("[webhook] build-event jobPath=%s type=%s result=%s", payload.JobPath, jobType, payload.Result)

	switch jobType {
	case "team-build", "platform-service-build":
		go h.coord.OnBuildComplete(payload)
	case "image-scan", "platform-service-scan":
		go h.coord.OnScanComplete(payload)
	case "source-scan":
		go h.coord.OnSourceScanComplete(payload)
	default:
		// not a tracked job — accept and discard
	}

	w.WriteHeader(http.StatusAccepted)
}
