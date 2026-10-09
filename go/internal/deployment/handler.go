package deployment

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"driftline/go/internal/models"
	"driftline/go/pkg/response"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes:
//
//	GET   /deployments?model=x            running first, then history
//	POST  /deployments                    {"model","version","traffic_percent"}
//	GET   /deployments/{id}
//	PATCH /deployments/{id}/traffic       {"traffic_percent": 25}   (canary only)
//	POST  /deployments/{id}/promote       canary -> primary at 100%
//	POST  /deployments/{id}/stop
//	POST  /deployments/rollback           {"model","reason"?}
func (h *Handler) Routes(r chi.Router) {
	r.Route("/deployments", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.deploy)
		r.Post("/rollback", h.rollback)
		r.Get("/{id}", h.get)
		r.Patch("/{id}/traffic", h.traffic)
		r.Post("/{id}/promote", h.promote)
		r.Post("/{id}/stop", h.stop)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ds, err := h.svc.List(r.Context(), r.URL.Query().Get("model"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	if ds == nil {
		ds = []models.Deployment{} // encode as [], not null
	}
	response.JSON(w, http.StatusOK, ds)
}

func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	var in DeployInput
	if err := response.Decode(r, &in); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	d, err := h.svc.Deploy(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, d)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, d)
}

func (h *Handler) traffic(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body struct {
		TrafficPercent int `json:"traffic_percent"`
	}
	if err := response.Decode(r, &body); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	d, err := h.svc.SetTraffic(r.Context(), id, body.TrafficPercent)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, d)
}

func (h *Handler) promote(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Promote(r.Context(), id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, d)
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Stop(r.Context(), id)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, d)
}

func (h *Handler) rollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string `json:"model"`
		Reason string `json:"reason"`
	}
	if err := response.Decode(r, &body); err != nil || body.Model == "" {
		response.Error(w, http.StatusBadRequest, `body must be {"model": "...", "reason"?: "..."}`)
		return
	}
	if body.Reason == "" {
		body.Reason = "manual rollback"
	}
	d, err := h.svc.Rollback(r.Context(), body.Model, body.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, d)
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid deployment id")
		return uuid.Nil, false
	}
	return id, true
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrConflict), errors.Is(err, ErrNothingToRollback):
		response.Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalid):
		response.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrWarmup):
		response.Error(w, http.StatusBadGateway, err.Error())
	default:
		slog.ErrorContext(r.Context(), "deployment request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		response.Error(w, http.StatusInternalServerError, "internal error")
	}
}
