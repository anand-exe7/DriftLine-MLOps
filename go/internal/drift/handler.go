package drift

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"driftline/go/internal/models"
	"driftline/go/pkg/response"
)

type Handler struct {
	repo      Repository
	scheduler *Scheduler
}

func NewHandler(repo Repository, scheduler *Scheduler) *Handler {
	return &Handler{repo: repo, scheduler: scheduler}
}

// Routes:
//
//	GET  /drift/reports?model=x&limit=50   newest first
//	GET  /drift/config                     thresholds, window, interval
//	POST /drift/run                        run a check now, return the reports
func (h *Handler) Routes(r chi.Router) {
	r.Get("/drift/reports", h.list)
	r.Get("/drift/config", h.config)
	r.Post("/drift/run", h.run)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	reports, err := h.repo.List(r.Context(), r.URL.Query().Get("model"), limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "list drift reports", "err", err)
		response.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if reports == nil {
		reports = []models.DriftReport{}
	}
	response.JSON(w, http.StatusOK, reports)
}

func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	c := h.scheduler.cfg
	response.JSON(w, http.StatusOK, map[string]any{
		"interval":            c.Interval.String(),
		"window":              c.Window.String(),
		"min_samples":         c.MinSamples,
		"feature_psi_trigger": c.Thresholds.FeaturePSI,
		"overall_psi_trigger": c.Thresholds.OverallPSI,
	})
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	reports, err := h.scheduler.RunOnce(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "manual drift run", "err", err)
		response.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if reports == nil {
		reports = []models.DriftReport{}
	}
	response.JSON(w, http.StatusOK, reports)
}
