package errortracking_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
)

func TestNotFoundNamesWhat(t *testing.T) {
	f := newFakeET(t)
	const notFound = `{"error":{"code":"not_found","message":"not found"}}`
	routes := []string{
		"GET projects/314/errors", "GET projects/314/errors/42", "GET projects/314/errors/42/events/abc",
		"PATCH projects/314/errors/42/comments/7", "POST projects/314/errors/42/review", "GET heatmaps/3",
	}
	for _, r := range routes {
		f.reply(r, 404, notFound)
	}
	notFoundCase := func(name string, args []string, route, details string, suggestions ...string) cliCase {
		return cliCase{name: name, args: args, calls: []string{route}, exit: gcxerrors.ExitGeneralError,
			check: func(t *testing.T, _ string, err error, _ []etCall) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, gcxerrors.SummaryResourceNotFound)
				assert.Contains(t, out, details+"\n")
				for _, s := range suggestions {
					assert.Contains(t, out, "• "+s+"\n")
				}
				assert.Equal(t, len(suggestions), strings.Count(out, "• "), "no other suggestions")
			}}
	}
	f.runCases(t, []cliCase{
		notFoundCase("a project", []string{"errors", "list", "--project", "314", "-o", "json"}, routes[0],
			"Error Tracking has no project 314", "List what exists: gcx error-tracking projects list"),
		notFoundCase("an error", []string{"errors", "get", "42", "--project", "314", "-o", "json"}, routes[1],
			"Error Tracking has no error 42 in project 314", "List what exists: gcx error-tracking errors list --project 314", "Check the project too: gcx error-tracking projects list"),
		notFoundCase("an event", []string{"errors", "events", "get", "42", "abc", "--project", "314", "-o", "json"}, routes[2],
			"Error Tracking has no event abc in error 42 in project 314", "List what exists: gcx error-tracking errors events list 42 --project 314", "Check the project too: gcx error-tracking projects list"),
		notFoundCase("a comment", []string{"errors", "comments", "update", "42", "7", "--project", "314", "--body", "x", "-o", "json"}, routes[3],
			"Error Tracking has no comment 7 in error 42 in project 314", "List what exists: gcx error-tracking errors activity list 42 --project 314 --type comment", "Check the project too: gcx error-tracking projects list"),
		notFoundCase("an error to review", []string{"errors", "update", "42", "--project", "314", "--reviewed", "-o", "json"}, routes[1],
			"Error Tracking has no error 42 in project 314", "List what exists: gcx error-tracking errors list --project 314", "Check the project too: gcx error-tracking projects list"),
		notFoundCase("a heatmap", []string{"heatmaps", "get", "3", "-o", "json"}, routes[5],
			"Error Tracking has no heatmap 3", "List what exists: gcx error-tracking heatmaps list"),
	})
}
