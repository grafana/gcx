package login

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/grafana/gcx/internal/cloud"
)

// provisionParams holds the inputs for minting a dedicated sigil access-policy
// token against GCOM.
type provisionParams struct {
	Region      string
	StackID     int
	StackSlug   string
	PolicyName  string
	TokenName   string
	TokenExpiry string // RFC3339; empty means the token never expires
	Scopes      []string
}

// provisionOutcome describes what provisionToken did, for reporting.
type provisionOutcome struct {
	PolicyName   string
	TokenName    string
	Token        string // the secret; only populated on success
	PolicyReused bool   // an existing policy was reused instead of created
	TokenRotated bool   // an existing token of the same name was replaced
}

// provisionToken finds or creates the sigil access policy for the stack and
// mints a fresh token under it. If a token with the same name already exists it
// is rotated (deleted and recreated) so exactly one active token remains.
func provisionToken(ctx context.Context, client *cloud.GCOMClient, p provisionParams) (provisionOutcome, error) {
	realm := cloud.Realm{Type: "stack", Identifier: strconv.Itoa(p.StackID)}

	policy, reused, err := findOrCreatePolicy(ctx, client, p, realm)
	if err != nil {
		return provisionOutcome{}, err
	}

	tok, rotated, err := createOrRotateToken(ctx, client, p, policy.ID)
	if err != nil {
		return provisionOutcome{}, err
	}

	return provisionOutcome{
		PolicyName:   policy.Name,
		TokenName:    tok.Name,
		Token:        tok.Token,
		PolicyReused: reused,
		TokenRotated: rotated,
	}, nil
}

func findOrCreatePolicy(ctx context.Context, client *cloud.GCOMClient, p provisionParams, realm cloud.Realm) (cloud.AccessPolicy, bool, error) {
	created, err := client.CreateAccessPolicy(ctx, p.Region, cloud.CreateAccessPolicyRequest{
		Name:        p.PolicyName,
		DisplayName: "Sigil (AI Observability) — " + p.StackSlug,
		Scopes:      p.Scopes,
		Realms:      []cloud.Realm{realm},
	})
	if err == nil {
		return created, false, nil
	}

	// Name+org+region must be unique; on conflict, reuse the existing policy.
	var httpErr *cloud.GCOMHTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusConflict {
		existing, found, lerr := findPolicyByName(ctx, client, p.Region, p.PolicyName, realm)
		if lerr != nil {
			return cloud.AccessPolicy{}, false, lerr
		}
		if found {
			return existing, true, nil
		}
	}
	return cloud.AccessPolicy{}, false, fmt.Errorf("create access policy %q: %w", p.PolicyName, err)
}

func findPolicyByName(ctx context.Context, client *cloud.GCOMClient, region, name string, realm cloud.Realm) (cloud.AccessPolicy, bool, error) {
	policies, err := client.ListAccessPolicies(ctx, region)
	if err != nil {
		return cloud.AccessPolicy{}, false, fmt.Errorf("list access policies: %w", err)
	}
	for i := range policies {
		if policies[i].Name == name && realmMatches(policies[i].Realms, realm) {
			return policies[i], true, nil
		}
	}
	return cloud.AccessPolicy{}, false, nil
}

func realmMatches(realms []cloud.Realm, want cloud.Realm) bool {
	for _, r := range realms {
		if r.Type == want.Type && r.Identifier == want.Identifier {
			return true
		}
	}
	return false
}

func createOrRotateToken(ctx context.Context, client *cloud.GCOMClient, p provisionParams, policyID string) (cloud.Token, bool, error) {
	req := cloud.CreateTokenRequest{
		AccessPolicyID: policyID,
		Name:           p.TokenName,
		DisplayName:    p.TokenName,
		ExpiresAt:      p.TokenExpiry,
	}

	tok, err := client.CreateToken(ctx, p.Region, req)
	if err == nil {
		return tok, false, nil
	}

	// Token name is taken: rotate it so the freshly-minted secret is the only
	// active one (we can't read back an existing token's secret).
	var httpErr *cloud.GCOMHTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusConflict {
		existing, lerr := client.ListTokens(ctx, p.Region, policyID, p.TokenName)
		if lerr != nil {
			return cloud.Token{}, false, fmt.Errorf("rotate token %q: list existing: %w", p.TokenName, lerr)
		}
		for _, t := range existing {
			if t.Name != p.TokenName {
				continue
			}
			if derr := client.DeleteToken(ctx, p.Region, t.ID); derr != nil {
				return cloud.Token{}, false, fmt.Errorf("rotate token %q: delete existing: %w", p.TokenName, derr)
			}
		}
		tok, err = client.CreateToken(ctx, p.Region, req)
		if err != nil {
			return cloud.Token{}, false, fmt.Errorf("create token %q after rotation: %w", p.TokenName, err)
		}
		return tok, true, nil
	}
	return cloud.Token{}, false, fmt.Errorf("create token %q: %w", p.TokenName, err)
}

// isPermissionError reports whether err is a GCOM 401/403, i.e. the bootstrap
// token lacks the scopes required to provision (accesspolicies:write etc.).
func isPermissionError(err error) bool {
	var httpErr *cloud.GCOMHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden
	}
	return false
}
