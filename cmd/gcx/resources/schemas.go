package resources

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type schemasOpts struct {
	IO       cmdio.Options
	NoSchema bool
}

func (opts *schemasOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &tabCodec{wide: false})
	opts.IO.RegisterCustomCodec("wide", &tabCodec{wide: true})
	opts.IO.DefaultFormat("text")

	opts.IO.BindFlags(flags)
	flags.BoolVar(&opts.NoSchema, "no-schema", false, "Skip fetching OpenAPI spec schemas (faster, omits schema info and unlistable resource types)")
}

func (opts *schemasOpts) Validate() error {
	return opts.IO.Validate()
}

func listTypesCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &schemasOpts{}

	cmd := &cobra.Command{
		Use:   "list-types [RESOURCE_SELECTOR]",
		Args:  cobra.MaximumNArgs(1),
		Short: "List available Grafana API resource types",
		Long:  "List available Grafana API resource types and their schemas by querying a live Grafana instance. Requires a connection to Grafana. Use --no-schema to skip OpenAPI spec fetching for faster results. Optionally filter by a resource selector.",
		Example: `
	gcx resources list-types
	gcx resources list-types -o wide
	gcx resources list-types -o json
	gcx resources list-types -o yaml
	gcx resources list-types -o json --no-schema
	gcx resources list-types incidents
	gcx resources list-types incidents.v1alpha1.incident.ext.grafana.app -o json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate parses --json and --jq. Without the call the two
			// branches below never fire, and --json silently prints the full
			// nested output instead.
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			reg, err := discovery.NewDefaultRegistry(ctx, cfg)
			if err != nil {
				return err
			}

			// TODO: refactor this to return a k8s object list,
			// e.g. APIResourceList, or unstructured.UnstructuredList.
			// That way we can use the same code for rendering as for `resources get`.
			res := reg.SupportedResources().Sorted()

			// If a resource selector argument was provided, filter to matching descriptors.
			if len(args) > 0 {
				sels, parseErr := resources.ParseSelectors(args)
				if parseErr != nil {
					return fmt.Errorf("invalid resource selector: %w", parseErr)
				}
				filters, filterErr := reg.MakeFilters(discovery.MakeFiltersOptions{
					Selectors:            sels,
					PreferredVersionOnly: true,
				})
				if filterErr != nil {
					return fmt.Errorf("unknown resource %q: %w", args[0], filterErr)
				}
				matched := make(map[string]bool, len(filters))
				for _, f := range filters {
					matched[f.Descriptor.GroupVersionKind().String()] = true
				}
				var filtered resources.Descriptors
				for _, d := range res {
					if matched[d.GroupVersionKind().String()] {
						filtered = append(filtered, d)
					}
				}
				res = filtered
			}

			// --json ? discovery and --json <path>,<path> selection both run on
			// the descriptor list. Encode routes them, and descriptorEntry
			// declares the field set for both, so one unknown path fails
			// instead of printing a null.
			if opts.IO.JSONDiscovery || len(opts.IO.JSONFields) > 0 {
				entries := make([]descriptorEntry, 0, len(res))
				for _, d := range res {
					entries = append(entries, newDescriptorEntry(d))
				}
				return opts.IO.Encode(cmd.OutOrStdout(), descriptorList{Items: entries})
			}

			// Fetch schemas regardless of output format (Pattern 13: format-agnostic
			// data fetching). The --no-schema flag is the correct opt-out mechanism,
			// not the output format. Tabular codecs simply ignore the schema data.
			var schemas map[string]map[string]any
			if !opts.NoSchema {
				fetcher, fetchErr := discovery.NewSchemaFetcher(&cfg.Config)
				if fetchErr != nil {
					return fmt.Errorf("initializing schema fetcher: %w", fetchErr)
				}
				schemas, fetchErr = fetcher.FetchSpecSchemas(ctx, res)
				if fetchErr != nil {
					return fmt.Errorf("fetching schemas: %w", fetchErr)
				}
			}

			switch opts.IO.OutputFormat {
			case "json", "yaml", "agents":
				// Structured formats get the full nested shape including the
				// fetched schemas. "agents" must be here: the agent-mode
				// default previously fell into the tabular branch, silently
				// dropping every schema the command had just fetched.
				return opts.IO.Encode(cmd.OutOrStdout(), descriptorsToNested(res, schemas))
			default:
				// text/table/wide: tabular output.
				return opts.IO.Encode(cmd.OutOrStdout(), res)
			}
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// descriptorList carries the descriptors of list-types for --json field
// selection. It implements the output.ListEnvelope marker, so selection runs
// per descriptor — the same fields that --json list enumerates. A plain
// map[string]any envelope would put selection on the whole object instead,
// and every Descriptor field would then look absent.
type descriptorList struct {
	Items []descriptorEntry `json:"items"`
}

// ListItemsKey names the key that holds the descriptors.
func (descriptorList) ListItemsKey() string { return "items" }

// descriptorEntry is one descriptor of the --json output. The struct declares
// the field set, so gcx rejects a path that no descriptor carries instead of
// printing a null. A map element declares nothing, and every unknown path
// then looked valid.
type descriptorEntry struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Kind     string `json:"kind"`
	Singular string `json:"singular"`
	Plural   string `json:"plural"`
}

// newDescriptorEntry converts a Descriptor to an entry for field selection
// and discovery. Keys use camelCase to match common JSON conventions.
func newDescriptorEntry(d resources.Descriptor) descriptorEntry {
	return descriptorEntry{
		Group:    d.GroupVersion.Group,
		Version:  d.GroupVersion.Version,
		Kind:     d.Kind,
		Singular: d.Singular,
		Plural:   d.Plural,
	}
}

// descriptorsToNested builds a nested group → version → []resource map for
// JSON/YAML output. When schemas is non-nil, each resource entry includes a
// "schema" key, and resources without an OpenAPI spec schema are dropped —
// they typically represent unlistable sub-resources (connections, queryconvert)
// that cannot be used for CRUD operations.
func descriptorsToNested(descs resources.Descriptors, schemas map[string]map[string]any) map[string]any {
	// Use typed intermediate maps to avoid unchecked type assertions.
	type versionMap = map[string][]map[string]any
	groups := make(map[string]versionMap)

	for _, d := range descs {
		group := d.GroupVersion.Group
		version := d.GroupVersion.Version
		gvk := group + "/" + version + "/" + d.Kind

		entry := map[string]any{
			"kind":     d.Kind,
			"plural":   d.Plural,
			"singular": d.Singular,
		}

		if schemas != nil {
			schema, hasSchema := resolveSchema(schemas, gvk, d)
			if !hasSchema {
				// No schema → unlistable sub-resource; skip entirely.
				continue
			}
			entry["schema"] = schema
		}

		if groups[group] == nil {
			groups[group] = make(versionMap)
		}
		groups[group][version] = append(groups[group][version], entry)
	}

	// Convert to map[string]any for JSON/YAML encoding.
	result := make(map[string]any, len(groups))
	for group, versions := range groups {
		vm := make(map[string]any, len(versions))
		for version, entries := range versions {
			vm[version] = entries
		}
		result[group] = vm
	}

	return result
}

type tabCodec struct {
	wide bool
}

func (c *tabCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}

	return "text"
}

func (c *tabCodec) Encode(output io.Writer, input any) error {
	descs, ok := input.(resources.Descriptors)
	if !ok {
		return fmt.Errorf("expected resources.Descriptors, got %T", input)
	}

	noTruncate := terminal.NoTruncate()

	var t *style.TableBuilder
	if c.wide {
		t = style.NewTable("GROUP", "VERSION", "PLURAL", "SINGULAR", "KIND")
	} else {
		t = style.NewTable("GROUP", "VERSION", "PLURAL")
	}

	for _, r := range descs {
		gv := r.GroupVersion
		if c.wide {
			t.Row(
				sanitizeCell(gv.Group, noTruncate),
				sanitizeCell(gv.Version, noTruncate),
				sanitizeCell(r.Plural, noTruncate),
				sanitizeCell(r.Singular, noTruncate),
				sanitizeCell(r.Kind, noTruncate))
		} else {
			t.Row(
				sanitizeCell(gv.Group, noTruncate),
				sanitizeCell(gv.Version, noTruncate),
				sanitizeCell(r.Plural, noTruncate))
		}
	}

	return t.Render(output)
}

func (c *tabCodec) Decode(io.Reader, any) error {
	return errors.New("tab codec does not support decoding")
}

// resolveSchema looks up a schema for a resource, first from server-fetched
// schemas (K8s-discovered), then from provider-registered schemas via the
// global SchemaForGVK function.
// Returns the schema and true if found, or nil and false if no schema exists.
func resolveSchema(serverSchemas map[string]map[string]any, gvk string, d resources.Descriptor) (any, bool) {
	if s, ok := serverSchemas[gvk]; ok {
		return s, true
	}
	// Fall back to global provider-registered schema.
	provSchema := adapter.SchemaForGVK(d.GroupVersionKind())
	if provSchema == nil {
		return nil, false
	}
	var parsed map[string]any
	if err := json.Unmarshal(provSchema, &parsed); err != nil {
		return nil, false
	}
	return parsed, true
}
