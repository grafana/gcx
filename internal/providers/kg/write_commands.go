package kg

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// buildEntityWriteRequest assembles and validates an EntityWriteRequest from flags.
func buildEntityWriteRequest(domain, entityType, name string, scope, props map[string]string, ttl string) (EntityWriteRequest, error) {
	if err := validateWritableDomain(domain); err != nil {
		return EntityWriteRequest{}, err
	}
	if err := validateIdentifier(entityType, "type"); err != nil {
		return EntityWriteRequest{}, err
	}
	if name == "" {
		return EntityWriteRequest{}, errors.New("--name is required")
	}
	if err := validateKgKeys(scope, "scope"); err != nil {
		return EntityWriteRequest{}, err
	}
	if err := validateKgKeys(props, "property"); err != nil {
		return EntityWriteRequest{}, err
	}
	if err := validateNoScopePropertyOverlap(scope, props); err != nil {
		return EntityWriteRequest{}, err
	}
	ttlSeconds, err := parseTTL(ttl)
	if err != nil {
		return EntityWriteRequest{}, err
	}
	return EntityWriteRequest{
		Domain: domain, Type: entityType, Name: name,
		Scope: scope, Properties: props, TTLSeconds: &ttlSeconds,
	}, nil
}

type entityCreateOpts struct {
	IO         cmdio.Options
	file       string
	domain     string
	entityType string
	name       string
	scope      map[string]string
	property   map[string]string
	ttl        string
}

func (o *entityCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &EntityWriteTableCodec{})
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.file, "file", "f", "", "Input file (YAML/JSON), or '-' for stdin; mutually exclusive with flags")
	flags.StringVar(&o.domain, "domain", "", "Writable domain slug — a specific application domain such as 'irm' (required; 'kg' is reserved)")
	flags.StringVar(&o.entityType, "type", "", "Entity type (identifier; required)")
	flags.StringVar(&o.name, "name", "", "Entity name (required)")
	flags.StringToStringVar(&o.scope, "scope", nil, "Scope as key=value (repeatable or comma-separated; identity-significant)")
	flags.StringToStringVar(&o.property, "property", nil, "Property as key=value (repeatable or comma-separated)")
	flags.StringVar(&o.ttl, "ttl", "", "Time-to-live duration (e.g. 1h, 7d); omitted = never expire")
}

func newEntitiesCreateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &entityCreateOpts{}
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Create or update a custom entity (upsert) [experimental].",
		Long: `Create or update an API-origin entity in a writable domain.

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change. If the write API is not enabled on your stack, the
server returns an error explaining how to request access.

Identity is (type, name, scope) + domain; re-running with the same identity
updates the entity. Scope is optional but identity-significant.

With -f, the input may be a single object or a YAML/JSON array. Array entries
are processed in order as independent upserts: the operation is not atomic,
and entries already written stay written if a later entry fails.`,
		Example: `  gcx kg entities upsert --domain myapp --type Service --name checkout --scope env=prod --ttl 1h
  gcx kg entities upsert -f entity.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			reqs, err := opts.resolveRequests(cmd)
			if err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			responses := make([]*EntityWriteResponse, 0, len(reqs))
			var failure error
			for i := range reqs {
				resp, created, err := client.UpsertEntity(cmd.Context(), reqs[i])
				if err != nil {
					if len(responses) == 0 {
						// Nothing succeeded: the standard error path emits
						// the single error document.
						return err
					}
					// Partial failure: entries already written stay written
					// (documented non-atomicity). The failure detail goes to
					// stderr; stdout keeps exactly one document below.
					failure = fmt.Errorf("failed to upsert entity %q (%d/%d succeeded): %w",
						reqs[i].Name, len(responses), len(reqs), err)
					cmdio.EmitWarn(cmd.ErrOrStderr(), failure.Error())
					break
				}
				verb := "updated"
				if created {
					verb = "created"
				}
				cmdio.Success(cmd.ErrOrStderr(), "entity %q %s", reqs[i].Name, verb)
				responses = append(responses, resp)
			}
			// Exactly one stdout document: the echoed object for a
			// single-request invocation (historical shape), an array for
			// batch (-f array) input.
			var doc any = responses
			if len(reqs) == 1 {
				doc = responses[0]
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), doc); err != nil {
				return err
			}
			if failure != nil {
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, failure)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// resolveRequests returns the entity requests from either -f or flags (mutually exclusive).
func (o *entityCreateOpts) resolveRequests(cmd *cobra.Command) ([]EntityWriteRequest, error) {
	flagsUsed := o.domain != "" || o.entityType != "" || o.name != "" || len(o.scope) > 0 || len(o.property) > 0 || o.ttl != ""
	if o.file != "" {
		if flagsUsed {
			return nil, errors.New("--file is mutually exclusive with --domain/--type/--name/--scope/--property/--ttl")
		}
		return o.requestsFromFile(cmd)
	}
	req, err := buildEntityWriteRequest(o.domain, o.entityType, o.name, o.scope, o.property, o.ttl)
	if err != nil {
		return nil, err
	}
	return []EntityWriteRequest{req}, nil
}

func (o *entityCreateOpts) requestsFromFile(cmd *cobra.Command) ([]EntityWriteRequest, error) {
	data, err := readFileOrStdin(cmd, o.file)
	if err != nil {
		return nil, err
	}
	var list []EntityWriteRequest
	if err := yaml.Unmarshal(data, &list); err == nil && len(list) > 0 {
		return o.validateFileRequests(list)
	}
	var single EntityWriteRequest
	if err := yaml.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("failed to parse entity file: %w", err)
	}
	return o.validateFileRequests([]EntityWriteRequest{single})
}

func (o *entityCreateOpts) validateFileRequests(reqs []EntityWriteRequest) ([]EntityWriteRequest, error) {
	for i := range reqs {
		if err := validateWritableDomain(reqs[i].Domain); err != nil {
			return nil, err
		}
		if err := validateIdentifier(reqs[i].Type, "type"); err != nil {
			return nil, err
		}
		if reqs[i].Name == "" {
			return nil, errors.New("entity name is required")
		}
		if err := validateKgKeys(reqs[i].Scope, "scope"); err != nil {
			return nil, err
		}
		if err := validateKgKeys(reqs[i].Properties, "property"); err != nil {
			return nil, err
		}
		if err := validateNoScopePropertyOverlap(reqs[i].Scope, reqs[i].Properties); err != nil {
			return nil, err
		}
		// An absent ttlSeconds defaults to never-expire (-1), matching the flag
		// default; without this an omitted field would marshal as 0 = expire
		// immediately, silently discarding the entity.
		if reqs[i].TTLSeconds == nil {
			reqs[i].TTLSeconds = neverExpire()
		}
	}
	return reqs, nil
}

// neverExpire returns a pointer to the never-expire TTL sentinel (-1).
func neverExpire() *int64 {
	v := int64(-1)
	return &v
}

// EntityWriteTableCodec renders one or more EntityWriteResponses as an
// identity table (one row per upserted entity).
type EntityWriteTableCodec struct{}

func (c *EntityWriteTableCodec) Format() format.Format { return "table" }

func (c *EntityWriteTableCodec) Encode(w io.Writer, v any) error {
	var responses []*EntityWriteResponse
	switch resp := v.(type) {
	case *EntityWriteResponse:
		responses = []*EntityWriteResponse{resp}
	case []*EntityWriteResponse:
		responses = resp
	default:
		return errors.New("invalid data type for table codec: expected *EntityWriteResponse")
	}
	t := style.NewTable("DOMAIN", "TYPE", "NAME", "SCOPE")
	for _, resp := range responses {
		t.Row(resp.Domain, resp.Type, resp.Name, scopeStr(resp.Scope))
	}
	return t.Render(w)
}

func (c *EntityWriteTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newEntitiesDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	var (
		domain     string
		entityType string
		name       string
		scope      map[string]string
		force      bool
		ioOpts     cmdio.Options
	)
	cmd := &cobra.Command{
		Use:   "delete [Type--Name]",
		Short: "Delete a custom entity [experimental].",
		Long: `Delete an API-origin entity. Scope is part of the entity's identity, so it must
match the value used at upsert — omitting it targets the scope-less entity, and a
mismatch returns 404 (not found).

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ioOpts.Validate(); err != nil {
				return err
			}
			et, n, err := resolveEntityTypeAndName(cmd, args)
			if err != nil {
				return err
			}
			if err := validateIdentifier(et, "type"); err != nil {
				return err
			}
			if err := validateWritableDomain(domain); err != nil {
				return err
			}
			if err := validateKgKeys(scope, "scope"); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), force,
				fmt.Sprintf("Delete entity %s--%s in domain %q?", et, n, domain))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.DeleteEntity(cmd.Context(), domain, et, n, scope); err != nil {
				if asNotFound(err) {
					return fmt.Errorf("%w\nhint: scope is part of identity — verify --scope and --domain match the values used at upsert", err)
				}
				return err
			}
			return ioOpts.Encode(cmd.OutOrStdout(),
				cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: et, Name: n}))
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Writable domain slug — a specific application domain such as 'irm' (required)")
	cmd.Flags().StringVar(&entityType, "type", "", "Entity type (or use positional Type--Name)")
	cmd.Flags().StringVar(&name, "name", "", "Entity name (or use positional Type--Name)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().StringToStringVar(&scope, "scope", nil, "Scope as key=value (repeatable or comma-separated; must match upsert-time scope)")
	ioOpts.RegisterCustomCodec("text", singleMutationText(func(w io.Writer, m cmdio.SingleMutation) {
		cmdio.Success(w, "entity %s--%s deleted", m.Target.Kind, m.Target.Name)
	}))
	ioOpts.DefaultFormat("text")
	ioOpts.BindFlags(cmd.Flags())
	return cmd
}

// asNotFound reports whether err is a KG APIError with a 404 status.
func asNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// newRelationshipsCommand is the 'kg relationships' group.
func newRelationshipsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "relationships",
		Aliases: []string{"relationship", "rels"},
		Short:   "Manage custom Knowledge Graph relationships [experimental].",
	}
	cmd.AddCommand(newRelationshipsCreateCommand(loader), newRelationshipsDeleteCommand(loader))
	return cmd
}

// buildRelationshipWriteRequest assembles and validates a RelationshipWriteRequest.
func buildRelationshipWriteRequest(domain, relType, fromTok string, fromScope map[string]string, toTok string, toScope, props map[string]string, ttl string) (RelationshipWriteRequest, error) {
	if err := validateWritableDomain(domain); err != nil {
		return RelationshipWriteRequest{}, err
	}
	if err := validateIdentifier(relType, "type"); err != nil {
		return RelationshipWriteRequest{}, err
	}
	from, err := refFromTokenAndScope(fromTok, fromScope)
	if err != nil {
		return RelationshipWriteRequest{}, fmt.Errorf("--from: %w", err)
	}
	to, err := refFromTokenAndScope(toTok, toScope)
	if err != nil {
		return RelationshipWriteRequest{}, fmt.Errorf("--to: %w", err)
	}
	if err := validateKgKeys(props, "property"); err != nil {
		return RelationshipWriteRequest{}, err
	}
	ttlSeconds, err := parseTTL(ttl)
	if err != nil {
		return RelationshipWriteRequest{}, err
	}
	return RelationshipWriteRequest{
		Domain: domain, Type: relType, From: from, To: to, Properties: props, TTLSeconds: &ttlSeconds,
	}, nil
}

// refFromTokenAndScope parses a domain/Type/name token and attaches the
// pre-parsed scope map from the --from-scope/--to-scope flags.
func refFromTokenAndScope(token string, scope map[string]string) (EntityRef, error) {
	ref, err := parseEntityRefToken(token)
	if err != nil {
		return EntityRef{}, err
	}
	if err := validateDomain(ref.Domain); err != nil {
		return EntityRef{}, err
	}
	if err := validateIdentifier(ref.Type, "type"); err != nil {
		return EntityRef{}, err
	}
	if err := validateKgKeys(scope, "scope"); err != nil {
		return EntityRef{}, err
	}
	ref.Scope = scope
	return ref, nil
}

type relCreateOpts struct {
	IO        cmdio.Options
	file      string
	domain    string
	relType   string
	from      string
	fromScope map[string]string
	to        string
	toScope   map[string]string
	property  map[string]string
	ttl       string
}

func (o *relCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RelationshipWriteTableCodec{})
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.file, "file", "f", "", "Input file (YAML/JSON), or '-' for stdin; mutually exclusive with flags")
	flags.StringVar(&o.domain, "domain", "", "Writable domain slug for the edge — a specific application domain such as 'irm' (required)")
	flags.StringVar(&o.relType, "type", "", "Relationship type (identifier; required)")
	flags.StringVar(&o.from, "from", "", "Source entity ref as domain/Type/name (required)")
	flags.StringToStringVar(&o.fromScope, "from-scope", nil, "Scope for --from as key=value (repeatable or comma-separated)")
	flags.StringVar(&o.to, "to", "", "Target entity ref as domain/Type/name (required)")
	flags.StringToStringVar(&o.toScope, "to-scope", nil, "Scope for --to as key=value (repeatable or comma-separated)")
	flags.StringToStringVar(&o.property, "property", nil, "Property as key=value (repeatable or comma-separated)")
	flags.StringVar(&o.ttl, "ttl", "", "Time-to-live duration (e.g. 1h, 7d); omitted = never expire")
}

func newRelationshipsCreateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &relCreateOpts{}
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Create or update a custom relationship (upsert) [experimental].",
		Long: `Create or update an API-origin edge between two existing entities.
Both endpoints must already exist.

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change.

With -f, the input may be a single object or a YAML/JSON array. Array entries
are processed in order as independent upserts: the operation is not atomic,
and entries already written stay written if a later entry fails.`,
		Example: `  gcx kg relationships upsert --type CALLS --domain myapp \
    --from myapp/Service/checkout --to myapp/Service/cart --to-scope env=prod --ttl 1h
  gcx kg relationships upsert -f rel.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			reqs, err := opts.resolveRequests(cmd)
			if err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			responses := make([]*RelationshipWriteResponse, 0, len(reqs))
			var failure error
			for i := range reqs {
				resp, err := client.UpsertRelationship(cmd.Context(), reqs[i])
				if err != nil {
					if len(responses) == 0 {
						// Nothing succeeded: the standard error path emits
						// the single error document.
						return err
					}
					// Partial failure: entries already written stay written
					// (documented non-atomicity). The failure detail goes to
					// stderr; stdout keeps exactly one document below.
					failure = fmt.Errorf("failed to upsert relationship %q (%d/%d succeeded): %w",
						reqs[i].Type, len(responses), len(reqs), err)
					cmdio.EmitWarn(cmd.ErrOrStderr(), failure.Error())
					break
				}
				cmdio.Success(cmd.ErrOrStderr(), "relationship %q written", reqs[i].Type)
				responses = append(responses, resp)
			}
			// Exactly one stdout document: the echoed object for a
			// single-request invocation (historical shape), an array for
			// batch (-f array) input.
			var doc any = responses
			if len(reqs) == 1 {
				doc = responses[0]
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), doc); err != nil {
				return err
			}
			if failure != nil {
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, failure)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (o *relCreateOpts) resolveRequests(cmd *cobra.Command) ([]RelationshipWriteRequest, error) {
	flagsUsed := o.domain != "" || o.relType != "" || o.from != "" || o.to != "" ||
		len(o.fromScope) > 0 || len(o.toScope) > 0 || len(o.property) > 0 || o.ttl != ""
	if o.file != "" {
		if flagsUsed {
			return nil, errors.New("--file is mutually exclusive with --type/--domain/--from/--to/--from-scope/--to-scope/--property/--ttl")
		}
		return o.requestsFromFile(cmd)
	}
	req, err := buildRelationshipWriteRequest(o.domain, o.relType, o.from, o.fromScope, o.to, o.toScope, o.property, o.ttl)
	if err != nil {
		return nil, err
	}
	return []RelationshipWriteRequest{req}, nil
}

func (o *relCreateOpts) requestsFromFile(cmd *cobra.Command) ([]RelationshipWriteRequest, error) {
	data, err := readFileOrStdin(cmd, o.file)
	if err != nil {
		return nil, err
	}
	var list []RelationshipWriteRequest
	if err := yaml.Unmarshal(data, &list); err == nil && len(list) > 0 {
		return validateRelFileRequests(list)
	}
	var single RelationshipWriteRequest
	if err := yaml.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("failed to parse relationship file: %w", err)
	}
	return validateRelFileRequests([]RelationshipWriteRequest{single})
}

func validateRelFileRequests(reqs []RelationshipWriteRequest) ([]RelationshipWriteRequest, error) {
	for i := range reqs {
		if err := validateWritableDomain(reqs[i].Domain); err != nil {
			return nil, err
		}
		if err := validateIdentifier(reqs[i].Type, "type"); err != nil {
			return nil, err
		}
		if err := validateRelFileRef(reqs[i].From, "from"); err != nil {
			return nil, err
		}
		if err := validateRelFileRef(reqs[i].To, "to"); err != nil {
			return nil, err
		}
		if err := validateKgKeys(reqs[i].Properties, "property"); err != nil {
			return nil, err
		}
		if reqs[i].TTLSeconds == nil {
			reqs[i].TTLSeconds = neverExpire()
		}
	}
	return reqs, nil
}

// validateRelFileRef validates a relationship endpoint ref from file input,
// matching the checks the flag path applies via refFromTokenAndScope.
func validateRelFileRef(ref EntityRef, side string) error {
	if err := validateDomain(ref.Domain); err != nil {
		return fmt.Errorf("%s.%w", side, err)
	}
	if err := validateIdentifier(ref.Type, side+".type"); err != nil {
		return err
	}
	if ref.Name == "" {
		return fmt.Errorf("relationship %s.name is required", side)
	}
	return validateKgKeys(ref.Scope, side+".scope")
}

// RelationshipWriteTableCodec renders one or more RelationshipWriteResponses
// as a table (one row per upserted edge).
type RelationshipWriteTableCodec struct{}

func (c *RelationshipWriteTableCodec) Format() format.Format { return "table" }

func (c *RelationshipWriteTableCodec) Encode(w io.Writer, v any) error {
	var responses []*RelationshipWriteResponse
	switch resp := v.(type) {
	case *RelationshipWriteResponse:
		responses = []*RelationshipWriteResponse{resp}
	case []*RelationshipWriteResponse:
		responses = resp
	default:
		return errors.New("invalid data type for table codec: expected *RelationshipWriteResponse")
	}
	t := style.NewTable("TYPE", "FROM", "TO")
	for _, resp := range responses {
		t.Row(resp.Type,
			fmt.Sprintf("%s/%s/%s", resp.From.Domain, resp.From.Type, resp.From.Name),
			fmt.Sprintf("%s/%s/%s", resp.To.Domain, resp.To.Type, resp.To.Name))
	}
	return t.Render(w)
}

func (c *RelationshipWriteTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func newRelationshipsDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	var (
		relType   string
		from      string
		fromScope map[string]string
		to        string
		toScope   map[string]string
		force     bool
		ioOpts    cmdio.Options
	)
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a custom relationship [experimental].",
		Long: `Delete an API-origin edge of the given type between the from/to entities.
The endpoint refs (incl. scope) must match the values used at upsert.

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change.`,
		Example: `  gcx kg relationships delete --type CALLS \
    --from myapp/Service/checkout --to myapp/Service/cart --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ioOpts.Validate(); err != nil {
				return err
			}
			if err := validateIdentifier(relType, "type"); err != nil {
				return err
			}
			fromRef, err := refFromTokenAndScope(from, fromScope)
			if err != nil {
				return fmt.Errorf("--from: %w", err)
			}
			toRef, err := refFromTokenAndScope(to, toScope)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), force,
				fmt.Sprintf("Delete relationship %q from %s to %s?", relType, from, to))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.DeleteRelationship(cmd.Context(), relType, fromRef, toRef); err != nil {
				if asNotFound(err) {
					return fmt.Errorf("%w\nhint: refs are part of identity — verify --from/--to (incl. scope) match the values used at upsert", err)
				}
				return err
			}
			return ioOpts.Encode(cmd.OutOrStdout(),
				cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "Relationship", Name: relType}))
		},
	}
	cmd.Flags().StringVar(&relType, "type", "", "Relationship type (identifier; required)")
	cmd.Flags().StringVar(&from, "from", "", "Source entity ref as domain/Type/name (required)")
	cmd.Flags().StringToStringVar(&fromScope, "from-scope", nil, "Scope for --from as key=value (repeatable or comma-separated)")
	cmd.Flags().StringVar(&to, "to", "", "Target entity ref as domain/Type/name (required)")
	cmd.Flags().StringToStringVar(&toScope, "to-scope", nil, "Scope for --to as key=value (repeatable or comma-separated)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	ioOpts.RegisterCustomCodec("text", singleMutationText(func(w io.Writer, m cmdio.SingleMutation) {
		cmdio.Success(w, "relationship %q deleted", m.Target.Name)
	}))
	ioOpts.DefaultFormat("text")
	ioOpts.BindFlags(cmd.Flags())
	return cmd
}
