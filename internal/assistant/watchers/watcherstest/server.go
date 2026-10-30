// Package watcherstest supplies wire fixtures for tests of Watcher consumers.
package watcherstest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/assistant/watchers"
)

// Server serves paged Watcher reads and records supplementary read requests.
// Configure its exported fields before starting an HTTP test server.
type Server struct {
	Current                  [][]watchers.Watcher
	Archived                 [][]watchers.Watcher
	CurrentStatus            int
	ArchivedStatus           int
	DetailStatus             map[string]int
	EnrollmentStatus         map[string]int
	Enrollment               map[string]bool
	EnrollmentDetails        map[string]watchers.Enrollment
	RawDetails               map[string]json.RawMessage
	AllowWrites              bool
	CreateStatus             int
	UpdateStatus             map[string]int
	DeleteStatus             map[string]int
	EnrollmentWriteStatus    map[string]int
	CalibrationRequestStatus int
	CalibrationResponse      json.RawMessage
	ErrorBody                string

	mu              sync.Mutex
	enrollmentReads map[string]int
	collectionReads map[bool]int
	mutationCalls   int
	mutations       []MutationRequest
	nextID          int
}

type MutationRequest struct {
	Method string
	Path   string
	Body   json.RawMessage
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.mutate(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	const prefix = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents"
	if r.URL.Path == prefix {
		s.list(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, prefix+"/") {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, prefix+"/")
	if id, ok := strings.CutSuffix(path, "/auto-calibration"); ok {
		if s.enrollmentReads == nil {
			s.enrollmentReads = make(map[string]int)
		}
		s.enrollmentReads[id]++
		if status := s.EnrollmentStatus[id]; status != 0 && status != http.StatusOK {
			http.Error(w, "fixture enrollment unavailable", status)
			return
		}
		if item, ok := s.EnrollmentDetails[id]; ok {
			write(w, item)
		} else {
			write(w, watchers.Enrollment{Enabled: s.Enrollment[id]})
		}
		return
	}
	if status := s.DetailStatus[path]; status != 0 && status != http.StatusOK {
		http.Error(w, "fixture detail unavailable", status)
		return
	}
	if body, ok := s.RawDetails[path]; ok {
		write(w, body)
		return
	}
	for _, partition := range [][][]watchers.Watcher{s.Current, s.Archived} {
		for _, page := range partition {
			for _, item := range page {
				if item.ID == path {
					write(w, item)
					return
				}
			}
		}
	}
	http.NotFound(w, r)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	archived := r.URL.Query().Get("archived") == "true"
	if s.collectionReads == nil {
		s.collectionReads = make(map[bool]int)
	}
	s.collectionReads[archived]++
	pages, status := s.Current, s.CurrentStatus
	if archived {
		pages, status = s.Archived, s.ArchivedStatus
	}
	if status != 0 && status != http.StatusOK {
		http.Error(w, "fixture collection unavailable", status)
		return
	}
	page := 0
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		var err error
		page, err = strconv.Atoi(strings.TrimPrefix(cursor, "page-"))
		if err != nil {
			http.Error(w, "unexpected cursor", http.StatusBadRequest)
			return
		}
	}
	items := []watchers.Watcher{}
	next := ""
	if page < len(pages) {
		items = pages[page]
		if page+1 < len(pages) {
			next = fmt.Sprintf("page-%d", page+1)
		}
	}
	write(w, map[string]any{"agents": items, "nextCursor": next})
}

func write(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) EnrollmentReads(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enrollmentReads[id]
}

func (s *Server) CollectionReads(archived bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.collectionReads[archived]
}

func (s *Server) MutationCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutationCalls
}

func (s *Server) MutationRequests() []MutationRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	requests := make([]MutationRequest, len(s.mutations))
	for i, request := range s.mutations {
		requests[i] = request
		requests[i].Body = append(json.RawMessage(nil), request.Body...)
	}
	return requests
}

func (s *Server) mutate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "unreadable request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutationCalls++
	s.mutations = append(s.mutations, MutationRequest{Method: r.Method, Path: r.URL.Path, Body: body})
	if !s.AllowWrites {
		http.Error(w, "mutations are forbidden by the fixture", http.StatusMethodNotAllowed)
		return
	}
	const prefix = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents"
	path := strings.TrimPrefix(r.URL.Path, prefix+"/")
	if strings.HasSuffix(path, "/initial-calibration") && r.Method == http.MethodPost {
		if s.fail(w, s.CalibrationRequestStatus) {
			return
		}
		if s.CalibrationResponse != nil {
			_, _ = w.Write(s.CalibrationResponse)
		} else {
			write(w, map[string]any{"status": "queued", "startWhenReady": false})
		}
		return
	}
	if id, ok := strings.CutSuffix(path, "/auto-calibration"); ok && r.Method == http.MethodPut {
		if s.fail(w, s.EnrollmentWriteStatus[id]) {
			return
		}
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if json.Unmarshal(body, &input) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if s.Enrollment == nil {
			s.Enrollment = make(map[string]bool)
		}
		s.Enrollment[id] = input.Enabled
		item := s.EnrollmentDetails[id]
		item.Enabled = input.Enabled
		if s.EnrollmentDetails != nil {
			s.EnrollmentDetails[id] = item
		}
		write(w, item)
		return
	}
	if r.URL.Path == prefix && r.Method == http.MethodPost {
		if s.fail(w, s.CreateStatus) {
			return
		}
		var request watchers.ConfigurationRequest
		if json.Unmarshal(body, &request) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		s.nextID++
		item := watchers.Watcher{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", s.nextID), Status: "draft", DefinitionVersion: 1}
		apply(&item, &request)
		if len(s.Current) == 0 {
			s.Current = [][]watchers.Watcher{{}}
		}
		s.Current[0] = append(s.Current[0], item)
		w.WriteHeader(http.StatusCreated)
		write(w, item)
		return
	}
	if r.Method == http.MethodPut && s.fail(w, s.UpdateStatus[path]) {
		return
	}
	if r.Method == http.MethodDelete && s.fail(w, s.DeleteStatus[path]) {
		return
	}
	for partitionIndex, partition := range [][][]watchers.Watcher{s.Current, s.Archived} {
		for pageIndex, page := range partition {
			for index := range page {
				if page[index].ID != path {
					continue
				}
				switch r.Method {
				case http.MethodDelete:
					page = append(page[:index], page[index+1:]...)
					if partitionIndex == 0 {
						s.Current[pageIndex] = page
					} else {
						s.Archived[pageIndex] = page
					}
					delete(s.RawDetails, path)
					write(w, map[string]bool{"deleted": true})
				case http.MethodPut:
					var request watchers.ConfigurationRequest
					if json.Unmarshal(body, &request) != nil {
						http.Error(w, "invalid request", http.StatusBadRequest)
						return
					}
					apply(&page[index], &request)
					write(w, page[index])
				default:
					http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
				}
				return
			}
		}
	}
	http.NotFound(w, r)
}

func (s *Server) fail(w http.ResponseWriter, status int) bool {
	if status == 0 || status >= 200 && status < 300 {
		return false
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, s.ErrorBody)
	return true
}

func apply(item *watchers.Watcher, input *watchers.ConfigurationRequest) {
	if input.Name != nil {
		item.Name = *input.Name
	}
	if input.Description != nil {
		item.Description = *input.Description
	}
	if input.Prompt != nil {
		item.Prompt = *input.Prompt
	}
	if input.TriggerIntervalSeconds != nil {
		item.TriggerIntervalSeconds = *input.TriggerIntervalSeconds
	}
	if input.Sensitivity != nil {
		item.Sensitivity = *input.Sensitivity
	}
	if input.DisableDecisionSkip != nil {
		item.DisableDecisionSkip = *input.DisableDecisionSkip
	}
	if input.DatasourceUIDs != nil {
		item.DatasourceUIDs = *input.DatasourceUIDs
	}
	if input.Labels != nil {
		item.Labels = *input.Labels
	}
	if input.AutoStop != nil {
		item.AutoStop = input.AutoStop
	}
	if input.Actions != nil {
		applyActions(item, input.Actions)
	}
}

func applyActions(item *watchers.Watcher, actions *watchers.WriteActions) {
	item.Actions.Slack = actions.Slack
	item.Actions.MSTeams = actions.MSTeams
	item.Actions.Investigation = actions.Investigation
	if actions.Alerting != nil {
		item.Actions.Alerting = actions.Alerting
	}
	if actions.Webhook != nil {
		applyWebhook(item, actions.Webhook)
	}
}

func applyWebhook(item *watchers.Watcher, a *watchers.WebhookWriteAction) {
	if item.Actions.Webhook == nil {
		item.Actions.Webhook = &watchers.WebhookAction{}
	}
	dst := item.Actions.Webhook
	dst.Enabled = false
	if a.Enabled != nil {
		dst.Enabled = *a.Enabled
	}
	if a.MinSeverity != nil {
		dst.MinSeverity = *a.MinSeverity
	}
	if a.AuthorizationScheme != nil {
		dst.AuthorizationScheme = *a.AuthorizationScheme
	}
	if dst.SecureFields == nil {
		dst.SecureFields = &watchers.SecureFields{}
	}
	if a.URL != nil {
		dst.SecureFields.URL = *a.URL != ""
	}
	if a.SecureSettings == nil {
		return
	}
	if a.SecureSettings.HMACSecret != nil {
		dst.SecureFields.HMACSecret = *a.SecureSettings.HMACSecret != ""
	}
	if a.SecureSettings.AuthorizationCredentials != nil {
		dst.SecureFields.AuthorizationCredentials = *a.SecureSettings.AuthorizationCredentials != ""
	}
}
