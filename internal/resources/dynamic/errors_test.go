package dynamic_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientdynamic "k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func TestParseStatusError(t *testing.T) {
	statusErr := &apierrors.StatusError{ErrStatus: metav1.Status{
		Status: metav1.StatusFailure, Code: http.StatusForbidden,
		Reason: metav1.StatusReasonForbidden, Message: "access denied",
	}}
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus bool
	}{
		{name: "nil"},
		{name: "ordinary error", err: errors.New("request failed")},
		{name: "transport error", err: &url.Error{Op: "Get", URL: "http://example.invalid", Err: errors.New("connection refused")}},
		{name: "canceled", err: context.Canceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
		{name: "wrapped canceled", err: fmt.Errorf("request: %w", context.Canceled)},
		{name: "API status", err: statusErr, wantStatus: true},
		{name: "wrapped API status", err: fmt.Errorf("request: %w", statusErr), wantStatus: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dynamic.ParseStatusError(tc.err)
			if !tc.wantStatus {
				require.Equal(t, tc.err, got, "non-status errors must be returned unchanged")
				var status apierrors.APIStatus
				require.NotErrorAs(t, got, &status)
				return
			}
			var status apierrors.APIStatus
			require.ErrorAs(t, got, &status)
			require.Equal(t, statusErr.ErrStatus, status.Status())
			require.EqualError(t, got, "403 Forbidden: access denied")
		})
	}
}

func TestNamespacedClientPreservesTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		canceled bool
	}{
		{name: "connection refused"},
		{name: "canceled context", canceled: true},
	} {
		for _, operation := range []string{"get", "list", "list with limit"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				server := httptest.NewServer(http.NotFoundHandler())
				server.Close()
				client, err := clientdynamic.NewForConfig(&rest.Config{Host: server.URL, Timeout: time.Second})
				require.NoError(t, err)
				namespaced := dynamic.NewNamespacedClient("default", client)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if tc.canceled {
					cancel()
				}
				switch operation {
				case "get":
					_, err = namespaced.Get(ctx, testDescriptor(), "some-uid", metav1.GetOptions{})
				case "list":
					_, err = namespaced.List(ctx, testDescriptor(), metav1.ListOptions{})
				case "list with limit":
					_, err = namespaced.List(ctx, testDescriptor(), metav1.ListOptions{Limit: 1})
				}
				require.Error(t, err)
				var status apierrors.APIStatus
				require.NotErrorAs(t, err, &status, "transport failures must not acquire an API status")
				if tc.canceled {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					var urlErr *url.Error
					require.ErrorAs(t, err, &urlErr)
				}
			})
		}
	}
}
