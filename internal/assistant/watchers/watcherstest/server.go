// Package watcherstest supplies wire fixtures for tests of Watcher consumers.
package watcherstest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/assistant/watchers"
)

// Server serves paged Watcher reads and records supplementary read requests.
// Configure its exported fields before starting an HTTP test server.
type Server struct {
	Current          [][]watchers.Watcher
	Archived         [][]watchers.Watcher
	CurrentStatus    int
	ArchivedStatus   int
	DetailStatus     map[string]int
	EnrollmentStatus map[string]int
	Enrollment       map[string]bool

	mu              sync.Mutex
	enrollmentReads map[string]int
	collectionReads map[bool]int
	mutationCalls   int
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.mu.Lock()
		s.mutationCalls++
		s.mu.Unlock()
		http.Error(w, "mutations are forbidden by the fixture", http.StatusMethodNotAllowed)
		return
	}
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
		s.mu.Lock()
		if s.enrollmentReads == nil {
			s.enrollmentReads = make(map[string]int)
		}
		s.enrollmentReads[id]++
		s.mu.Unlock()
		if status := s.EnrollmentStatus[id]; status != 0 && status != http.StatusOK {
			http.Error(w, "fixture enrollment unavailable", status)
			return
		}
		write(w, watchers.Enrollment{Enabled: s.Enrollment[id]})
		return
	}
	if status := s.DetailStatus[path]; status != 0 && status != http.StatusOK {
		http.Error(w, "fixture detail unavailable", status)
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
	s.mu.Lock()
	if s.collectionReads == nil {
		s.collectionReads = make(map[bool]int)
	}
	s.collectionReads[archived]++
	s.mu.Unlock()
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
