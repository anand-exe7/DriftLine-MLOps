package prediction

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/pkg/response"
)

type Handler struct {
	svc  *Service
	pool *pgxpool.Pool // for read-only stats queries
}

func NewHandler(svc *Service, pool *pgxpool.Pool) *Handler {
	return &Handler{svc: svc, pool: pool}
}

// Routes:
//
//	POST /predict/{model}         {"features": {...}, "request_id"?, "routing_key"?}
//	GET  /predict/{model}/routes  current routing table (versions + weights)
//	GET  /predict/{model}/stats?window=1h  logged traffic per version
func (h *Handler) Routes(r chi.Router) {
	r.Post("/predict/{model}", h.predict)
	r.Get("/predict/{model}/routes", h.routes)
	r.Get("/predict/{model}/stats", h.stats)
}

func (h *Handler) predict(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req Request
	if err := response.Decode(r, &req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	res, err := h.svc.Predict(r.Context(), chi.URLParam(r, "model"), req)
	switch {
	case err == nil:
		response.JSON(w, http.StatusOK, res)
	case errors.Is(err, ErrInvalid):
		response.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNoRoute):
		response.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrUnavailable):
		response.Error(w, http.StatusBadGateway, err.Error())
	default:
		slog.ErrorContext(r.Context(), "predict failed", "err", err)
		response.Error(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) routes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.svc.Router().Routes(r.Context(), chi.URLParam(r, "model"))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if routes == nil {
		routes = []Route{}
	}
	response.JSON(w, http.StatusOK, routes)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	window := time.Hour
	if v, err := time.ParseDuration(r.URL.Query().Get("window")); err == nil && v > 0 && v <= 30*24*time.Hour {
		window = v
	}
	stats, err := StatsSince(r.Context(), h.pool, chi.URLParam(r, "model"), time.Now().Add(-window))
	if err != nil {
		slog.ErrorContext(r.Context(), "prediction stats failed", "err", err)
		response.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if stats == nil {
		stats = []Stats{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"window": window.String(), "versions": stats})
}
