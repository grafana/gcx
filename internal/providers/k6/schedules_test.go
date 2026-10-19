//nolint:testpackage // Command and adapter tests need the unexported constructors.
package k6

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// scheduleAPI fakes k6 Cloud's documented schedule routes and nothing else.
// A load test has at most one schedule, a POST to the load test's schedule
// route replaces it, and any other route gets k6's unknown-route 404 body.
type scheduleAPI struct {
	mu         sync.Mutex
	byTest     map[int]map[string]any // load test ID -> schedule
	nextID     int
	failDelete int // schedule ID whose DELETE fails with a 500
}

func newScheduleAPI(schedules ...map[string]any) *scheduleAPI {
	api := &scheduleAPI{byTest: map[int]map[string]any{}, nextID: 100}
	for _, s := range schedules {
		api.byTest[intField(s, "load_test_id")] = s
	}
	return api
}

func intField(m map[string]any, key string) int {
	v, _ := m[key].(int)
	return v
}

// scheduleFor returns the load test's current schedule, or nil.
func (a *scheduleAPI) scheduleFor(loadTestID int) map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.byTest[loadTestID]
}

func (a *scheduleAPI) findByID(id int) (int, map[string]any) {
	for loadTestID, s := range a.byTest {
		if s["id"] == id {
			return loadTestID, s
		}
	}
	return 0, nil
}

func (a *scheduleAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cloud/v6/schedules", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		list := make([]map[string]any, 0, len(a.byTest))
		for _, s := range a.byTest {
			list = append(list, s)
		}
		slices.SortFunc(list, func(x, y map[string]any) int { return intField(x, "id") - intField(y, "id") })
		writeStatusJSON(w, http.StatusOK, map[string]any{"value": list})
	})
	mux.HandleFunc("GET /cloud/v6/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		id, _ := strconv.Atoi(r.PathValue("id"))
		if _, s := a.findByID(id); s != nil {
			writeStatusJSON(w, http.StatusOK, s)
			return
		}
		writeObjectNotFound(w, r.PathValue("id"))
	})
	mux.HandleFunc("DELETE /cloud/v6/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		id, _ := strconv.Atoi(r.PathValue("id"))
		if id == a.failDelete {
			writeStatusJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": "boom"}})
			return
		}
		loadTestID, s := a.findByID(id)
		if s == nil {
			writeObjectNotFound(w, r.PathValue("id"))
			return
		}
		delete(a.byTest, loadTestID)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /cloud/v6/load_tests/{id}/schedule", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		loadTestID, _ := strconv.Atoi(r.PathValue("id"))
		if s := a.byTest[loadTestID]; s != nil {
			writeStatusJSON(w, http.StatusOK, s)
			return
		}
		writeObjectNotFound(w, r.PathValue("id"))
	})
	mux.HandleFunc("POST /cloud/v6/load_tests/{id}/schedule", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		loadTestID, _ := strconv.Atoi(r.PathValue("id"))
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStatusJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		s := map[string]any{
			"load_test_id": loadTestID, "starts": body["starts"], "deactivated": false,
			"recurrence_rule": body["recurrence_rule"], "cron": body["cron"],
		}
		if old := a.byTest[loadTestID]; old != nil {
			s["id"] = old["id"]
		} else {
			s["id"] = a.nextID
			a.nextID++
		}
		a.byTest[loadTestID] = s
		writeStatusJSON(w, http.StatusCreated, s)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeStatusJSON(w, http.StatusNotFound, map[string]any{"error_msg": "404 Route Not Found"})
	})
	return mux
}

func writeStatusJSON(w http.ResponseWriter, status int, v map[string]any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeObjectNotFound(w http.ResponseWriter, id string) {
	writeStatusJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]any{"message": "Resource matching query does not exist: '" + id + "'", "code": "error"},
	})
}

// cronSchedule is schedule 10 of load test 5, running on a cron expression.
func cronSchedule() map[string]any {
	return map[string]any{
		"id": 10, "load_test_id": 5, "starts": "2026-06-01T10:00:00Z", "deactivated": false,
		"cron":            map[string]any{"schedule": "20 13 * * *", "time_zone": "Europe/London"},
		"recurrence_rule": nil, "next_run": "2026-06-01T12:20:00Z",
	}
}

// weeklySchedule is schedule 11 of load test 6, recurring every two weeks.
func weeklySchedule() map[string]any {
	return map[string]any{
		"id": 11, "load_test_id": 6, "starts": "2026-06-01T10:00:00Z", "deactivated": false,
		"cron": nil,
		"recurrence_rule": map[string]any{
			"frequency": "WEEKLY", "interval": 2, "byday": []any{"MO", "FR"},
			"until": "2026-12-31T00:00:00Z", "count": 4,
		},
	}
}

func k6ClientNames() []string { return []string{"ProxyClient", "DirectClient"} }

// newTestClient returns the named k6 client talking to handler.
func newTestClient(t *testing.T, name string, handler http.Handler) API {
	t.Helper()
	if name == "ProxyClient" {
		srv := httptest.NewServer(http.StripPrefix(pluginProxyBasePath, handler))
		t.Cleanup(srv.Close)
		return NewProxyClient(t.Context(), srv.URL, &http.Client{})
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := NewDirectClient(t.Context(), srv.URL, nil)
	client.SetCachedAuth("test-token", 1, 999)
	return client
}

// newTestLoader returns a command loader whose client talks to handler. It
// serves the token exchange too, because the seeded auth cache may be rejected.
func newTestLoader(t *testing.T, handler http.Handler) *mockLoader {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/v3/account/grafana-app/start" {
			writeStatusJSON(w, http.StatusOK, map[string]any{"organization_id": "42", "v3_grafana_token": "cached-v3"})
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return &mockLoader{
		cloudCfg:   providers.CloudRESTConfig{Stack: cloud.StackInfo{ID: 999}, Namespace: "stack-999"},
		grafanaCfg: config.NamespacedRESTConfig{Config: rest.Config{BearerToken: "glsa_test"}},
		providerCfg: map[string]string{
			"api-domain": srv.URL, keyCachedToken: "cached-v3", keyCachedOrgID: "42", keyCachedStackID: "999",
		},
	}
}

func scheduleDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: APIGroup, Version: APIVersionStr},
		Kind:         "Schedule", Singular: "schedule", Plural: "schedules",
	}
}

func TestSchedules_ReadEveryRecurrenceSetting(t *testing.T) {
	wantCron := &ScheduleCron{Schedule: "20 13 * * *", TimeZone: "Europe/London"}
	wantRule := &RecurrenceRule{Frequency: "WEEKLY", Interval: 2, ByDay: []string{"MO", "FR"}, Until: "2026-12-31T00:00:00Z", Count: 4}
	for _, name := range k6ClientNames() {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, name, newScheduleAPI(cronSchedule(), weeklySchedule()).handler())

			cron, err := client.GetSchedule(t.Context(), 10)
			require.NoError(t, err)
			assert.Equal(t, wantCron, cron.Cron)
			assert.Nil(t, cron.RecurrenceRule)

			weekly, err := client.GetSchedule(t.Context(), 11)
			require.NoError(t, err)
			assert.Nil(t, weekly.Cron)
			assert.Equal(t, wantRule, weekly.RecurrenceRule)

			all, err := client.ListSchedules(t.Context())
			require.NoError(t, err)
			require.Len(t, all, 2)
			assert.Equal(t, wantCron, all[0].Cron)
			assert.Equal(t, wantRule, all[1].RecurrenceRule)
		})
	}
}

func TestSchedulesGet_ShowsRecurrenceSettings(t *testing.T) {
	loader := newTestLoader(t, newScheduleAPI(cronSchedule(), weeklySchedule()).handler())

	stdout, _, err := runK6Command(t, false, loader, newSchedulesGetCommand, []string{"10", "-o", "yaml"}, "")
	require.NoError(t, err)
	assert.Contains(t, stdout, "20 13 * * *")
	assert.Contains(t, stdout, "time_zone: Europe/London")

	stdout, _, err = runK6Command(t, false, loader, newSchedulesGetCommand, []string{"11", "-o", "json"}, "")
	require.NoError(t, err)
	doc := decodeSingleJSON(t, stdout)
	assert.Equal(t, "WEEKLY", lookup(doc, "recurrence_rule.frequency"))
	assert.Equal(t, []any{"MO", "FR"}, lookup(doc, "recurrence_rule.byday"))
	assert.Equal(t, "2026-12-31T00:00:00Z", lookup(doc, "recurrence_rule.until"))
	assert.EqualValues(t, 4, lookup(doc, "recurrence_rule.count"))
}

func TestSchedulesList_ShowsRecurrenceSettings(t *testing.T) {
	loader := newTestLoader(t, newScheduleAPI(cronSchedule(), weeklySchedule()).handler())

	stdout, _, err := runK6Command(t, false, loader, newSchedulesListCommand, []string{"-o", "json"}, "")
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &list), stdout)
	require.Len(t, list, 2)
	assert.Equal(t, "20 13 * * *", lookup(list[0], "cron.schedule"))
	assert.Equal(t, "Europe/London", lookup(list[0], "cron.time_zone"))
	assert.Equal(t, []any{"MO", "FR"}, lookup(list[1], "recurrence_rule.byday"))
}

func TestSchedules_UpdateReplacesThroughLoadTestRoute(t *testing.T) {
	newCron := &ScheduleCron{Schedule: "0 10 * * *", TimeZone: "UTC"}
	for _, name := range k6ClientNames() {
		t.Run(name, func(t *testing.T) {
			api := newScheduleAPI(cronSchedule(), weeklySchedule())
			client := newTestClient(t, name, api.handler())

			updated, err := client.UpdateScheduleByID(t.Context(), 11, ScheduleRequest{
				Starts: "2026-07-01T00:00:00Z", Cron: newCron,
			})
			require.NoError(t, err)
			assert.Equal(t, newCron, updated.Cron)

			stored := api.scheduleFor(6)
			assert.Equal(t, map[string]any{"schedule": "0 10 * * *", "time_zone": "UTC"}, stored["cron"])
			assert.Nil(t, stored["recurrence_rule"])
			assert.Equal(t, "2026-07-01T00:00:00Z", stored["starts"])
		})
	}
}

func TestSchedulesUpdate_CronSchedule(t *testing.T) {
	loader := newTestLoader(t, newScheduleAPI(cronSchedule()).handler())
	input := "starts: \"2026-07-01T00:00:00Z\"\ncron:\n  schedule: \"0 10 * * *\"\n  time_zone: UTC\n"
	_, _, err := runK6Command(t, false, loader, newSchedulesUpdateCommand, []string{"10", "-f", "-"}, input)
	require.NoError(t, err)

	stdout, _, err := runK6Command(t, false, loader, newSchedulesListCommand, []string{"-o", "json"}, "")
	require.NoError(t, err)
	var list []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &list), stdout)
	require.Len(t, list, 1)
	assert.EqualValues(t, 5, list[0]["load_test_id"])
	assert.Equal(t, "0 10 * * *", lookup(list[0], "cron.schedule"))
	assert.Equal(t, "UTC", lookup(list[0], "cron.time_zone"))
}

func TestScheduleAdapter_PullPushKeepsCron(t *testing.T) {
	api := newScheduleAPI(cronSchedule())
	crud := newScheduleCRUD(newTestClient(t, "DirectClient", api.handler()), "stack-999", scheduleDescriptor())

	pulled, err := crud.Get(t.Context(), "10", metav1.GetOptions{})
	require.NoError(t, err)
	// Round-trip through JSON, as a pulled file is read back for push.
	raw, err := json.Marshal(pulled.Object)
	require.NoError(t, err)
	var pushed unstructured.Unstructured
	require.NoError(t, json.Unmarshal(raw, &pushed.Object))

	_, err = crud.Update(t.Context(), &pushed, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"schedule": "20 13 * * *", "time_zone": "Europe/London"}, api.scheduleFor(5)["cron"])
}

func TestSchedules_DeleteByScheduleID(t *testing.T) {
	for _, name := range k6ClientNames() {
		t.Run(name, func(t *testing.T) {
			api := newScheduleAPI(cronSchedule(), weeklySchedule())
			api.failDelete = 11
			client := newTestClient(t, name, api.handler())

			require.NoError(t, client.DeleteSchedule(t.Context(), 10))
			assert.Nil(t, api.scheduleFor(5))
			require.NoError(t, client.DeleteSchedule(t.Context(), 10), "a schedule that is already gone counts as deleted")

			require.Error(t, client.DeleteSchedule(t.Context(), 11))
			assert.NotNil(t, api.scheduleFor(6))
		})
	}
}

func TestSchedules_GetLoadTestSchedule(t *testing.T) {
	for _, name := range k6ClientNames() {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, name, newScheduleAPI(weeklySchedule()).handler())

			s, err := client.GetLoadTestSchedule(t.Context(), 6)
			require.NoError(t, err)
			assert.Equal(t, 11, s.ID)

			_, err = client.GetLoadTestSchedule(t.Context(), 5)
			require.ErrorIs(t, err, errNoSchedule)
		})
	}
}

func TestLoadTestsDeleteSchedule(t *testing.T) {
	t.Run("scheduled load test", func(t *testing.T) {
		api := newScheduleAPI(cronSchedule())
		loader := newTestLoader(t, api.handler())
		stdout, _, err := runK6Command(t, false, loader, newTestsDeleteScheduleCommand, []string{"5"}, "")
		require.NoError(t, err)
		assert.Contains(t, stdout, "Deleted schedule for load test 5")

		listed, _, err := runK6Command(t, false, loader, newSchedulesListCommand, []string{"-o", "json"}, "")
		require.NoError(t, err)
		assert.Equal(t, "[]", strings.TrimSpace(listed))
	})
	t.Run("load test without a schedule", func(t *testing.T) {
		api := newScheduleAPI(weeklySchedule())
		loader := newTestLoader(t, api.handler())
		_, _, err := runK6Command(t, false, loader, newTestsDeleteScheduleCommand, []string{"5"}, "")
		require.NoError(t, err)
		assert.NotNil(t, api.scheduleFor(6), "other load tests' schedules are untouched")
	})
	t.Run("delete fails upstream", func(t *testing.T) {
		api := newScheduleAPI(cronSchedule())
		api.failDelete = 10
		loader := newTestLoader(t, api.handler())
		_, _, err := runK6Command(t, false, loader, newTestsDeleteScheduleCommand, []string{"5"}, "")
		require.Error(t, err)
		assert.NotNil(t, api.scheduleFor(5))
	})
}

func TestScheduleAdapter_DeleteUsesScheduleRoute(t *testing.T) {
	api := newScheduleAPI(cronSchedule())
	crud := newScheduleCRUD(newTestClient(t, "DirectClient", api.handler()), "stack-999", scheduleDescriptor())
	require.NoError(t, crud.Delete(t.Context(), "10", metav1.DeleteOptions{}))
	assert.Nil(t, api.scheduleFor(5))
}

func TestScheduleAdapter_GetShowsRecurrenceSettings(t *testing.T) {
	client := newTestClient(t, "DirectClient", newScheduleAPI(cronSchedule()).handler())
	obj, err := newScheduleCRUD(client, "stack-999", scheduleDescriptor()).Get(t.Context(), "10", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "20 13 * * *", lookup(obj.Object, "spec.cron.schedule"))
	assert.Equal(t, "Europe/London", lookup(obj.Object, "spec.cron.time_zone"))
}
