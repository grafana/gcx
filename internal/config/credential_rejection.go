package config

import (
	"github.com/grafana/gcx/internal/credentials"
)

func (stack *StackConfig) rejectCredential(field credentials.Field, reason string) {
	if stack == nil {
		return
	}
	if stack.credentialRejections == nil {
		stack.credentialRejections = map[credentials.Field]CredentialRejectedError{}
	}
	stack.credentialRejections[field] = CredentialRejectedError{
		Source: stack.sourceIdentity,
		Owner:  credentials.StackOwner(stack.Name),
		Field:  field,
		Reason: reason,
	}
}

func (stack *StackConfig) clearCredentialRejection(field credentials.Field) {
	if stack != nil {
		delete(stack.credentialRejections, field)
	}
}

func (stack *StackConfig) credentialRejection(field credentials.Field) error {
	if stack == nil {
		return nil
	}
	rejection, ok := stack.credentialRejections[field]
	if !ok {
		return nil
	}
	return rejection
}

func (entry *CloudEntry) rejectCredential(field credentials.Field, reason string) {
	if entry == nil {
		return
	}
	if entry.credentialRejections == nil {
		entry.credentialRejections = map[credentials.Field]CredentialRejectedError{}
	}
	entry.credentialRejections[field] = CredentialRejectedError{
		Source: entry.sourceIdentity,
		Owner:  credentials.CloudOwner(entry.Name),
		Field:  field,
		Reason: reason,
	}
}

func (entry *CloudEntry) clearCredentialRejection(field credentials.Field) {
	if entry != nil {
		delete(entry.credentialRejections, field)
	}
}

func (entry *CloudEntry) credentialRejection(field credentials.Field) error {
	if entry == nil {
		return nil
	}
	rejection, ok := entry.credentialRejections[field]
	if !ok {
		return nil
	}
	return rejection
}

// GrafanaCredentialRejection returns a rejection for the authentication mode
// the resolved context intends to use. For legacy configs without an explicit
// auth method, rejection evidence participates in the same precedence as live
// credentials: OAuth, then service-account token, then Basic authentication.
// This prevents a withheld higher-priority credential from silently degrading
// to a lower-priority credential after its runtime value was cleared.
func (context *Context) GrafanaCredentialRejection() error {
	if context == nil || context.Grafana == nil {
		return nil
	}
	selection, err := context.selectGrafanaAuth()
	if err != nil {
		return err
	}
	return context.grafanaCredentialRejectionForMode(selection.mode)
}

func (context *Context) grafanaCredentialRejectionForMode(mode grafanaAuthMode) error {
	if context == nil || context.StackEntry == nil {
		return nil
	}
	switch mode {
	case grafanaAuthOAuth:
		return firstCredentialRejection(context.StackEntry, credentials.FieldOAuthToken, credentials.FieldOAuthRefreshToken)
	case grafanaAuthToken:
		return firstCredentialRejection(context.StackEntry, credentials.FieldGrafanaToken)
	case grafanaAuthBasic:
		return firstCredentialRejection(context.StackEntry, credentials.FieldGrafanaPassword)
	default:
		return nil
	}
}

func firstCredentialRejection(stack *StackConfig, fields ...credentials.Field) error {
	for _, field := range fields {
		if err := stack.credentialRejection(field); err != nil {
			return err
		}
	}
	return nil
}

// DirectProviderCredentialRejection protects provider-specific direct auth.
// Only token-shaped fields managed by the central credential store belong
// here; other provider validation remains provider-owned.
func (context *Context) DirectProviderCredentialRejection(providerName, key string) error {
	if context == nil || context.StackEntry == nil {
		return nil
	}
	if providerName == "synth" && key == "sm-token" {
		return context.StackEntry.credentialRejection(credentials.FieldSMToken)
	}
	return nil
}
