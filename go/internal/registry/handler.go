package registry

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"driftline/go/internal/models"
	"driftline/go/pkg/response"
)

type Handler struct {
	svc            *Service
	maxUploadBytes int64
}

func NewHandler(svc *Service, maxUploadBytes int64) *Handler {
	return &Handler{svc: svc, maxUploadBytes: maxUploadBytes}
}

// Routes mounts the registry API, e.g. under /api/v1.
//
//	POST /models                                      create a model
//	GET  /models                                      list models
//	GET  /models/{name}                               get a model
//	POST /models/{name}/versions                      upload a version (multipart)
//	GET  /models/{name}/versions                      list versions
//	GET  /models/{name}/versions/{version}            get a version
//	POST /models/{name}/versions/{version}/promote    {"stage": "production"}
//	GET  /models/{name}/versions/{version}/artifact   download model.onnx
func (h *Handler) Routes(r chi.Router) {
	r.Route("/models", func(r chi.Router) {
		r.Post("/", h.createModel)
		r.Get("/", h.listModels)
		r.Route("/{name}", func(r chi.Router) {
			r.Get("/", h.getModel)
			r.Post("/versions", h.registerVersion)
			r.Get("/versions", h.listVersions)
			r.Get("/versions/{version}", h.getVersion)
			r.Post("/versions/{version}/promote", h.promote)
			r.Get("/versions/{version}/artifact", h.downloadArtifact)
		})
	})
}

func (h *Handler) createModel(w http.ResponseWriter, r *http.Request) {
	var in CreateModelInput
	if err := response.Decode(r, &in); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	m, err := h.svc.CreateModel(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, m)
}

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	ms, err := h.svc.ListModels(r.Context())
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, ms)
}

func (h *Handler) getModel(w http.ResponseWriter, r *http.Request) {
	m, err := h.svc.GetModel(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, m)
}

// registerVersion expects multipart/form-data with:
//
//	version         text, required   e.g. "v2"
//	model           file, required   model.onnx
//	feature_schema  file, required   feature_schema.json
//	metrics         file, optional   evaluation metrics JSON
//	baseline_stats  file, optional   baseline_stats.json for drift detection
func (h *Handler) registerVersion(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	// Parts beyond 32 MiB spill to temp files instead of RAM.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			response.Error(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("upload exceeds %d bytes", h.maxUploadBytes))
			return
		}
		response.Error(w, http.StatusBadRequest, "expected multipart/form-data: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()

	model, header, err := r.FormFile("model")
	if err != nil {
		response.Error(w, http.StatusBadRequest, `missing "model" file part`)
		return
	}
	defer model.Close()

	schema, err := readPart(r, "feature_schema", true)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	metrics, err := readPart(r, "metrics", false)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	baseline, err := readPart(r, "baseline_stats", false)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	v, err := h.svc.RegisterVersion(r.Context(), RegisterVersionInput{
		ModelName:     chi.URLParam(r, "name"),
		Version:       r.FormValue("version"),
		Artifact:      model,
		ArtifactSize:  header.Size,
		FeatureSchema: schema,
		Metrics:       metrics,
		BaselineStats: baseline,
	})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusCreated, v)
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := h.svc.ListVersions(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, vs)
}

func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.GetVersion(r.Context(), chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, v)
}

type promoteRequest struct {
	Stage models.Stage `json:"stage"`
}

func (h *Handler) promote(w http.ResponseWriter, r *http.Request) {
	var req promoteRequest
	if err := response.Decode(r, &req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	v, err := h.svc.Promote(r.Context(), chi.URLParam(r, "name"), chi.URLParam(r, "version"), req.Stage)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	response.JSON(w, http.StatusOK, v)
}

func (h *Handler) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	v, rc, err := h.svc.DownloadArtifact(r.Context(), chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.onnx"`, v.ModelName, v.Version))
	w.Header().Set("X-Checksum-SHA256", v.ChecksumSHA256)
	if _, err := io.Copy(w, rc); err != nil {
		// Headers are already sent, so the client just sees a truncated body.
		return
	}
}

// readPart accepts a field either as a file part or a plain form value.
func readPart(r *http.Request, field string, required bool) ([]byte, error) {
	if f, _, err := r.FormFile(field); err == nil {
		defer f.Close()
		return io.ReadAll(f)
	} else if !errors.Is(err, http.ErrMissingFile) {
		return nil, fmt.Errorf("read %q: %w", field, err)
	}
	if v := r.FormValue(field); v != "" {
		return []byte(v), nil
	}
	if required {
		return nil, fmt.Errorf("missing %q part", field)
	}
	return nil, nil
}

// writeErr maps service errors to HTTP status codes. Unknown errors become a
// generic 500 so internal details (SQL, hostnames) never leak to clients.
func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrConflict):
		response.Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalid):
		response.Error(w, http.StatusBadRequest, err.Error())
	default:
		slog.ErrorContext(r.Context(), "registry: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		response.Error(w, http.StatusInternalServerError, "internal error")
	}
}
