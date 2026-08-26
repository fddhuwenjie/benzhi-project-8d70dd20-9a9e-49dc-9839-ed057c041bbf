package server

import (
	"embed"
	"errors"
	"field-voice-archive/internal/domain"
	"field-voice-archive/internal/workflow"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type API struct {
	workflow *workflow.Service
	mux      *http.ServeMux
}

func New(service *workflow.Service) (*API, error) {
	a := &API{workflow: service, mux: http.NewServeMux()}
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	a.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	a.mux.HandleFunc("GET /", a.HandleHome)
	a.mux.HandleFunc("GET /api/v1/batches", a.HandleListBatches)
	a.mux.HandleFunc("POST /api/v1/batches", a.HandleCreateBatch)
	a.mux.HandleFunc("PATCH /api/v1/batches/{batchID}", a.HandleUpdateBatch)
	a.mux.HandleFunc("GET /api/v1/batches/{batchID}", a.HandleGetBatch)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/consents", a.HandleAddConsent)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/consents/withdraw", a.HandleWithdrawConsent)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/consents/withdrawal", a.HandleWithdrawConsent)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/annotations", a.HandleAnnotate)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/redactions/preview", a.HandleRedactionPreview)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/redactions", a.HandleRedact)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/reviews", a.HandleReview)
	a.mux.HandleFunc("POST /api/v1/batches/{batchID}/release", a.HandleRelease)
	a.mux.HandleFunc("GET /api/v1/batches/{batchID}/audit", a.HandleAudit)
	a.mux.HandleFunc("GET /api/v1/batches/{batchID}/evidence", a.HandleEvidence)
	a.mux.HandleFunc("GET /api/v1/batches/{batchID}/manifest", a.HandleManifest)
	a.mux.HandleFunc("POST /api/v1/batches/verify", a.HandleVerifyPublished)
	a.mux.HandleFunc("GET /api/v1/batches/verify", a.HandleVerifyPublishedGet)
	return a, nil
}

func (a *API) Handler() http.Handler { return securityHeaders(a.mux) }

func (a *API) HandleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (a *API) HandleListBatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	filter := workflow.ListFilter{Status: strings.TrimSpace(q.Get("status")), LanguageVariant: strings.TrimSpace(q.Get("language_variant")), CollectionSite: strings.TrimSpace(q.Get("collection_site")), CollectedFrom: q.Get("collected_from"), CollectedTo: q.Get("collected_to"), Query: q.Get("q"), ReleasedOnly: q.Get("released_only") == "true", Page: page, PageSize: size}
	if filter.CollectedFrom != "" && filter.CollectedTo != "" && filter.CollectedFrom > filter.CollectedTo {
		writeError(w, errors.New("日期范围无效"))
		return
	}
	for _, d := range []string{filter.CollectedFrom, filter.CollectedTo} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				writeError(w, errors.New("日期参数无效"))
				return
			}
		}
	}
	if filter.Status != "" {
		switch domain.Status(filter.Status) {
		case domain.StatusDraft, domain.StatusConsented, domain.StatusAnnotated, domain.StatusRedacted, domain.StatusRejected, domain.StatusApproved, domain.StatusReleased:
		default:
			writeError(w, errors.New("非法 status 筛选值"))
			return
		}
	}
	result, err := a.workflow.ListFiltered(filter)
	if err != nil {
		writeError(w, err)
		return
	}
	for i := range result.Batches {
		cov := result.Batches[i].Coverage(time.Now())
		stats := result.Batches[i].SegmentStats()
		if result.Batches[i].Status == domain.StatusReleased {
			result.Batches[i] = *result.Batches[i].PublicCopy()
		}
		result.Batches[i].ConsentCoverage, result.Batches[i].SegmentStatistics = &cov, &stats
		result.Batches[i].PendingRequiredChanges = result.Batches[i].PendingChanges()
	}
	writeJSON(w, http.StatusOK, result)
}
