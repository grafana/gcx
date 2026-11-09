package login //nolint:testpackage // White-box test: needs access to unexported validator struct for stub injection.

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestValidateReportsRejectedOIDCToken(t *testing.T) {
	unauthorized := func(context.Context, config.NamespacedRESTConfig) error {
		return k8sapierrors.NewUnauthorized("jwt rejected")
	}
	unavailable := func(context.Context, config.NamespacedRESTConfig) error {
		return k8sapierrors.NewNotFound(schema.GroupResource{Resource: "apis"}, "")
	}
	v12 := semver.MustParse("12.0.0")

	t.Run("401 with OIDC names the ID token", func(t *testing.T) {
		v := &validator{grafana: &stubGrafanaClient{version: v12}, discovery: unauthorized}
		opts := Options{Inputs: Inputs{Server: "https://grafana.example.com", UseOIDC: true, OIDCIssuer: "https://idp.example.com"}}
		_, err := v.validate(context.Background(), opts, config.NamespacedRESTConfig{})
		rejected, ok := errors.AsType[*OIDCTokenRejectedError](err)
		require.True(t, ok, "got %T: %v", err, err)
		assert.Equal(t, "https://idp.example.com", rejected.Issuer)
	})

	t.Run("401 with another method stays a discovery error", func(t *testing.T) {
		v := &validator{grafana: &stubGrafanaClient{version: v12}, discovery: unauthorized}
		_, err := v.validate(context.Background(), Options{Inputs: Inputs{GrafanaToken: "glsa"}}, config.NamespacedRESTConfig{})
		_, ok := errors.AsType[*K8sDiscoveryError](err)
		assert.True(t, ok, "got %T: %v", err, err)
	})

	t.Run("other OIDC failures stay discovery errors", func(t *testing.T) {
		v := &validator{grafana: &stubGrafanaClient{version: v12}, discovery: unavailable}
		_, err := v.validate(context.Background(), Options{Inputs: Inputs{UseOIDC: true}}, config.NamespacedRESTConfig{})
		_, ok := errors.AsType[*K8sDiscoveryError](err)
		assert.True(t, ok, "got %T: %v", err, err)
	})
}
