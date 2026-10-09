package alert

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"driftline/go/internal/models"
	"driftline/go/pkg/response"
)

type Handler struct {
	svc  *Service
	repo Repository
}

func NewHandler(svc *Service, repo Repository) *Handler {
	return &Handler{svc: svc, repo: repo}
}

// Routes:
//
//	GET  /alerts?limit=50   recent alert attempts
//	POST /alerts/test       send a sample drift alert to every configured channel
func (h *Handler) Routes(r chi.Router) {
	r.Get("/alerts", h.list)
	r.Post("/alerts/test", h.test)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	alerts, err := h.repo.ListRecent(r.Context(), limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	response.JSON(w, http.StatusOK, alerts)
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	if len(h.svc.Channels()) == 0 {
		response.Error(w, http.StatusPreconditionFailed, "no alert channels configured (set SLACK_WEBHOOK_URL / DISCORD_WEBHOOK_URL)")
		return
	}
	end := time.Now()
	ks := 0.21
	report := models.DriftReport{
		ModelName:     "test_model",
		Version:       "v0",
		WindowStart:   end.Add(-time.Hour),
		WindowEnd:     end,
		SampleSize:    1000,
		OverallPSI:    0.27,
		DriftDetected: true,
		FeatureScores: map[string]models.FeatureDrift{
			"Income":       {PSI: 0.41, KSStat: &ks},
			"InterestRate": {PSI: 0.18},
			"Age":          {PSI: 0.02},
		},
	}
	if err := h.svc.SendTest(r.Context(), report); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"sent_to": h.svc.Channels()})
}
