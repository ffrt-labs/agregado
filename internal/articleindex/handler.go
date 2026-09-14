package articleindex

import (
	"encoding/json"
	"net/http"
)

type Handler struct {
	secret  string
	service *Service
}

func NewHandler(secret string, service *Service) *Handler {
	return &Handler{secret: secret, service: service}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.secret == "" || r.Header.Get("X-Enrichment-Secret") != h.secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var request Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	record, created, err := h.service.Process(r.Context(), request)
	if err != nil {
		if request.validate() != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, "enrichment failed", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if created {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(map[string]any{"id": record.ID, "status": record.Status, "created": created})
}
