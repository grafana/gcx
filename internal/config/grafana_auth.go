package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/credentials"
)

type grafanaAuthMode uint8

const (
	grafanaAuthUnknown grafanaAuthMode = iota
	grafanaAuthOAuth
	grafanaAuthToken
	grafanaAuthBasic
	grafanaAuthMTLS
)

func (mode grafanaAuthMode) String() string {
	switch mode {
	case grafanaAuthOAuth:
		return "oauth"
	case grafanaAuthToken:
		return "token"
	case grafanaAuthBasic:
		return "basic"
	case grafanaAuthMTLS:
		return "mtls"
	default:
		return "unknown"
	}
}

type grafanaAuthSelection struct {
	mode     grafanaAuthMode
	explicit bool
}

// selectGrafanaAuth is the sole authority for choosing Grafana HTTP auth.
// Explicit auth-method values win over stale fields. Legacy configs retain the
// transport's historical OAuth > token > Basic > mTLS/anonymous precedence,
// while rejection evidence keeps a cleared higher-priority credential from
// silently degrading to a lower-priority one.
func selectGrafanaAuth(grafana *GrafanaConfig, stack *StackConfig, contextName string) (grafanaAuthSelection, error) {
	if grafana == nil {
		return grafanaAuthSelection{mode: grafanaAuthUnknown}, nil
	}

	explicit := strings.TrimSpace(grafana.AuthMethod)
	if explicit != "" {
		switch strings.ToLower(explicit) {
		case "oauth":
			return grafanaAuthSelection{mode: grafanaAuthOAuth, explicit: true}, nil
		case "token":
			return grafanaAuthSelection{mode: grafanaAuthToken, explicit: true}, nil
		case "basic":
			return grafanaAuthSelection{mode: grafanaAuthBasic, explicit: true}, nil
		case "mtls":
			return grafanaAuthSelection{mode: grafanaAuthMTLS, explicit: true}, nil
		default:
			return grafanaAuthSelection{}, ValidationError{
				Path:    fmt.Sprintf("$.stacks.'%s'.grafana.auth-method", contextName),
				Message: fmt.Sprintf("unsupported auth-method %q", explicit),
				Suggestions: []string{
					"Use one of: oauth, token, basic, mtls",
					"Or remove auth-method to use legacy credential inference",
				},
			}
		}
	}

	if hasCredentialRejection(stack, credentials.FieldOAuthToken, credentials.FieldOAuthRefreshToken) ||
		grafana.ProxyEndpoint != "" || grafana.OAuthToken != "" || grafana.OAuthRefreshToken != "" {
		return grafanaAuthSelection{mode: grafanaAuthOAuth}, nil
	}
	if hasCredentialRejection(stack, credentials.FieldGrafanaToken) || grafana.APIToken != "" {
		return grafanaAuthSelection{mode: grafanaAuthToken}, nil
	}
	if hasCredentialRejection(stack, credentials.FieldGrafanaPassword) || grafana.User != "" || grafana.Password != "" {
		return grafanaAuthSelection{mode: grafanaAuthBasic}, nil
	}
	if hasTLSClientCertificate(grafana.TLS) || hasTLSClientPrivateKey(grafana.TLS) {
		return grafanaAuthSelection{mode: grafanaAuthMTLS}, nil
	}
	return grafanaAuthSelection{mode: grafanaAuthUnknown}, nil
}

// selectGrafanaAuth returns the runtime auth selection for this resolved
// context. A non-blank GRAFANA_TOKEN is explicit invocation intent and must win
// over a persisted auth-method from another mode. The marker lives only on the
// resolved Context, so this derived selector can never be serialized back into
// the stack's auth-method field.
func (context *Context) selectGrafanaAuth() (grafanaAuthSelection, error) {
	if context != nil && context.runtimeSecretOverrides[credentials.FieldGrafanaToken] {
		return grafanaAuthSelection{mode: grafanaAuthToken, explicit: true}, nil
	}
	if context == nil {
		return grafanaAuthSelection{mode: grafanaAuthUnknown}, nil
	}
	return selectGrafanaAuth(context.Grafana, context.StackEntry, context.stackName())
}

// EffectiveGrafanaAuthMethod returns the validated auth mode selected for this
// context. A non-blank runtime GRAFANA_TOKEN wins for this invocation; otherwise
// explicit auth-method values are authoritative, and an empty method uses legacy
// inference with credential-rejection evidence included in precedence. It fails
// on a rejected credential or missing selected-mode material, so consumers that
// cannot reuse ToRESTConfig can safely dispatch on its result.
func (context *Context) EffectiveGrafanaAuthMethod() (string, error) {
	selection, err := context.validatedGrafanaAuthSelection()
	if err != nil {
		return "", err
	}
	return selection.mode.String(), nil
}

// EffectiveGrafanaTLS returns a deep-cloned TLS view authorized by the same
// validated auth selection as EffectiveGrafanaAuthMethod. Explicit non-mTLS
// methods strip stale client identity while preserving server trust settings.
// Callers may resolve file-backed settings or otherwise mutate the result
// without changing the loaded configuration.
func (context *Context) EffectiveGrafanaTLS() (*TLS, error) {
	selection, err := context.validatedGrafanaAuthSelection()
	if err != nil {
		return nil, err
	}
	return context.Grafana.tlsForSelectedAuth(selection), nil
}

func (context *Context) validatedGrafanaAuthSelection() (grafanaAuthSelection, error) {
	if context == nil {
		return grafanaAuthSelection{}, ValidationError{
			Path:    "$.contexts",
			Message: "context is required",
		}
	}
	if context.Grafana == nil {
		return grafanaAuthSelection{}, ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'", context.Name),
			Message: "context references no stack with grafana config",
		}
	}
	selection, err := context.selectGrafanaAuth()
	if err != nil {
		return grafanaAuthSelection{}, err
	}
	if err := context.grafanaCredentialRejectionForMode(selection.mode); err != nil {
		return grafanaAuthSelection{}, err
	}
	if err := context.Grafana.validateSelectedAuth(selection, context.stackName()); err != nil {
		return grafanaAuthSelection{}, err
	}
	return selection, nil
}

func hasCredentialRejection(stack *StackConfig, fields ...credentials.Field) bool {
	if stack == nil {
		return false
	}
	for _, field := range fields {
		if _, ok := stack.credentialRejections[field]; ok {
			return true
		}
	}
	return false
}

func hasTLSClientCertificate(tlsConfig *TLS) bool {
	return tlsConfig != nil && (len(tlsConfig.CertData) > 0 || strings.TrimSpace(tlsConfig.CertFile) != "")
}

func hasTLSClientPrivateKey(tlsConfig *TLS) bool {
	return tlsConfig != nil && (len(tlsConfig.KeyData) > 0 || strings.TrimSpace(tlsConfig.KeyFile) != "")
}

// tlsForSelectedAuth returns the transport TLS view authorized by selection.
// Explicit non-mTLS modes ignore stale client identity while retaining server
// trust settings. Legacy empty-method configs retain their historical ability
// to combine HTTP authentication with a client certificate.
func (grafana GrafanaConfig) tlsForSelectedAuth(selection grafanaAuthSelection) *TLS {
	if grafana.TLS == nil {
		return nil
	}
	if selection.explicit && selection.mode != grafanaAuthMTLS {
		return grafana.TLS.ServerTrustOnly()
	}
	return grafana.TLS.clone()
}

func (tlsConfig *TLS) clone() *TLS {
	if tlsConfig == nil {
		return nil
	}
	selected := *tlsConfig
	selected.CertData = slices.Clone(tlsConfig.CertData)
	selected.KeyData = slices.Clone(tlsConfig.KeyData)
	selected.CAData = slices.Clone(tlsConfig.CAData)
	selected.NextProtos = slices.Clone(tlsConfig.NextProtos)
	selected.credentialCertFile.contents = slices.Clone(tlsConfig.credentialCertFile.contents)
	selected.credentialKeyFile.contents = slices.Clone(tlsConfig.credentialKeyFile.contents)
	selected.credentialCAFile.contents = slices.Clone(tlsConfig.credentialCAFile.contents)
	return &selected
}

// ServerTrustOnly returns a deep clone containing TLS server-trust settings
// but no client certificate or private key. It is intended for pre-auth probes
// and other flows that have explicitly selected a non-mTLS authentication
// method before a fully resolved Context exists.
func (tlsConfig *TLS) ServerTrustOnly() *TLS {
	selected := tlsConfig.clone()
	if selected == nil {
		return nil
	}
	selected.CertFile = ""
	selected.KeyFile = ""
	selected.CertData = nil
	selected.KeyData = nil
	selected.credentialCertFile = tlsFileSnapshot{}
	selected.credentialKeyFile = tlsFileSnapshot{}
	return selected
}

func (grafana GrafanaConfig) validateSelectedAuth(selection grafanaAuthSelection, contextName string) error {
	path := fmt.Sprintf("$.stacks.'%s'.grafana.auth-method", contextName)
	switch selection.mode {
	case grafanaAuthOAuth:
		if strings.TrimSpace(grafana.ProxyEndpoint) == "" ||
			(strings.TrimSpace(grafana.OAuthToken) == "" && strings.TrimSpace(grafana.OAuthRefreshToken) == "") {
			return ValidationError{
				Path:    path,
				Message: `OAuth authentication requires proxy-endpoint and at least one of oauth-token or oauth-refresh-token`,
				Suggestions: []string{
					"Run `gcx login --oauth` to complete the OAuth flow",
					"Or remove partial OAuth fields and choose another auth-method",
				},
			}
		}
	case grafanaAuthToken:
		if strings.TrimSpace(grafana.APIToken) == "" {
			return ValidationError{
				Path:    path,
				Message: `auth-method "token" requires a non-empty Grafana service-account token`,
				Suggestions: []string{
					"Set GRAFANA_TOKEN for this command",
					"Or run `gcx login --token <token>` to replace the stored credential",
				},
			}
		}
	case grafanaAuthBasic:
		missingUser := strings.TrimSpace(grafana.User) == ""
		missingPassword := selection.explicit && strings.TrimSpace(grafana.Password) == ""
		if missingUser || missingPassword {
			message := `Basic authentication requires a non-empty user`
			if selection.explicit {
				message = `auth-method "basic" requires a non-empty user and password`
			}
			return ValidationError{
				Path:    path,
				Message: message,
				Suggestions: []string{
					"Set both GRAFANA_USER and GRAFANA_PASSWORD",
					"Or choose token, oauth, or mtls authentication",
				},
			}
		}
	case grafanaAuthMTLS:
		if !hasTLSClientCertificate(grafana.TLS) || !hasTLSClientPrivateKey(grafana.TLS) {
			return ValidationError{
				Path:    path,
				Message: `auth-method "mtls" requires both a TLS client certificate and private key for this invocation; set GRAFANA_TLS_CERT_FILE and GRAFANA_TLS_KEY_FILE or persist both paths in the selected config`,
				Suggestions: []string{
					"Set both GRAFANA_TLS_CERT_FILE and GRAFANA_TLS_KEY_FILE for this command",
					fmt.Sprintf("Or persist stacks.%s.grafana.tls.cert-file and key-file in the selected config", contextName),
				},
			}
		}
	}
	return nil
}
