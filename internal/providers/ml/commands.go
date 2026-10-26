package ml

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

type commandOpts struct {
	IO       cmdio.Options
	Filename string
	Force    bool
	DryRun   bool
}

func (o *commandOpts) setup(flags *pflag.FlagSet, verb string) {
	if verb != "list" && verb != "get" {
		o.IO.DefaultFormat("json")
	}
	o.IO.BindFlags(flags)
	if verb == "create" || verb == "update" {
		flags.StringVarP(&o.Filename, "filename", "f", "", "JSON or YAML file, or - for stdin")
	}
	if verb == "delete" {
		flags.BoolVar(&o.Force, "force", false, "Delete without a confirmation prompt")
	}
	if verb == "create" || verb == "update" || verb == "delete" {
		flags.BoolVar(&o.DryRun, "dry-run", false, "Check the input without a server mutation; server validation is not available")
	}
}
func (o *commandOpts) Validate(verb string) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if (verb == "create" || verb == "update") && strings.TrimSpace(o.Filename) == "" {
		return errors.New("--filename is required (use - for stdin)")
	}
	return nil
}
func metadata(verb string) map[string]string {
	cost, hint := "small", ""
	if verb == "list" {
		cost = "large"
		hint = "Use get with a server ID for one resource. Use --json or --jq to select fields. The list includes all resources."
	}
	return map[string]string{agent.AnnotationTokenCost: cost, agent.AnnotationLLMHint: hint, agent.AnnotationAvailability: agent.AvailabilityCloudOnly}
}

func resourceCommands[T displayResource](loader providers.GrafanaConfigLoader, path, label string, r adapter.Resource[T]) *cobra.Command {
	group := &cobra.Command{Use: path, Short: "Manage Machine Learning " + label + "."}
	bound := providers.BindGrafanaResource(loader, r)
	for _, verb := range []string{"list", "get", "create", "update", "delete"} {
		opts := &commandOpts{}
		use := verb
		count := 0
		if verb == "get" || verb == "update" || verb == "delete" {
			use += " ID"
			count = 1
		}
		cmd := &cobra.Command{Use: use, Short: strings.ToUpper(verb[:1]) + verb[1:] + " Machine Learning " + label + ".", Example: "  gcx ml " + path + " list\n  gcx ml " + path + " get <id> -o yaml", Args: cobra.ExactArgs(count), Annotations: metadata(verb)}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(verb); err != nil {
				return err
			}
			if len(args) > 0 {
				if err := validID(args[0]); err != nil {
					return err
				}
			}
			var item T
			if verb == "create" || verb == "update" {
				if err := providers.ReadFileOrStdin(opts.Filename, cmd.InOrStdin(), &item); err != nil {
					return err
				}
				if strings.TrimSpace(item.displayName()) == "" {
					return errors.New("resource name is required")
				}
			}
			if opts.DryRun {
				result := cmdio.NewSingleMutation("skipped", cmdio.MutationTarget{Kind: r.Kind, Name: item.GetResourceName()})
				result.DryRun = true
				result.Error = adapter.ErrDryRunUnverified.Error()
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}
			if verb == "delete" {
				yes, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, "Delete ML resource "+args[0]+"?")
				if err != nil {
					return err
				}
				if !yes {
					return context.Canceled
				}
			}
			crud, _, err := bound.Load(cmd.Context())
			if err != nil {
				return err
			}
			switch verb {
			case "list":
				result, err := crud.List(cmd.Context(), 0)
				if err != nil {
					return err
				}
				items := make([]unstructured.Unstructured, 0, len(result))
				for _, obj := range result {
					clean, err := crud.ToUnstructured(obj.Spec)
					if err != nil {
						return err
					}
					items = append(items, clean)
				}
				return opts.IO.Encode(cmd.OutOrStdout(), items)
			case "get":
				result, err := crud.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				clean, err := crud.ToUnstructured(result.Spec)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), &clean)
			case "create", "update":
				obj := &adapter.TypedObject[T]{Spec: item}
				var result *adapter.TypedObject[T]
				if verb == "create" {
					result, err = crud.Create(cmd.Context(), obj)
				} else {
					result, err = crud.Update(cmd.Context(), args[0], obj)
				}
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation(verb+"d", cmdio.MutationTarget{Kind: r.Kind, Name: result.Name}))
			case "delete":
				if err = crud.Delete(cmd.Context(), args[0]); err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: r.Kind, Name: args[0]}))
			}
			return nil
		}
		registerReadTable(&opts.IO, verb, cmdio.Table[unstructured.Unstructured]{Columns: []cmdio.Column[unstructured.Unstructured]{
			{Header: "ID", Content: func(o unstructured.Unstructured) string { return o.GetName() }},
			{Header: "NAME", Content: func(o unstructured.Unstructured) string {
				name, _, _ := unstructured.NestedString(o.Object, "spec", "name")
				return name
			}},
		}})
		opts.setup(cmd.Flags(), verb)
		group.AddCommand(cmd)
	}
	return group
}

// Alerts require a parent ID. They have no standalone adapter or list endpoint.
func addAlertCommands(group *cobra.Command, loader providers.GrafanaConfigLoader, parent string) {
	for _, verb := range []string{"list", "get", "create", "update", "delete"} {
		opts := &commandOpts{}
		annotations := metadata(verb)
		if verb == "list" {
			annotations[agent.AnnotationLLMHint] = "Use get-alert with parent and alert server IDs for one alert. Use --json or --jq to select fields. The list includes all alerts for the parent."
		}
		count := 1
		use := verb + "-alerts PARENT_ID"
		if verb != "list" {
			use = verb + "-alert PARENT_ID"
		}
		if verb == "get" || verb == "update" || verb == "delete" {
			use += " ALERT_ID"
			count = 2
		}
		cmd := &cobra.Command{Use: use, Short: strings.ToUpper(verb[:1]) + verb[1:] + " alerts for one ML parent resource.", Args: cobra.ExactArgs(count), Annotations: annotations}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(verb); err != nil {
				return err
			}
			for _, id := range args {
				if err := validID(id); err != nil {
					return err
				}
			}
			var item Alert
			if verb == "create" || verb == "update" {
				if err := providers.ReadFileOrStdin(opts.Filename, cmd.InOrStdin(), &item); err != nil {
					return err
				}
				if strings.TrimSpace(item.Title) == "" {
					return errors.New("alert title is required")
				}
			}
			if opts.DryRun {
				result := cmdio.NewSingleMutation("skipped", cmdio.MutationTarget{Kind: "Alert", Name: item.ID})
				result.DryRun = true
				result.Error = adapter.ErrDryRunUnverified.Error()
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}
			if verb == "delete" {
				yes, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, "Delete ML alert "+args[1]+"?")
				if err != nil {
					return err
				}
				if !yes {
					return context.Canceled
				}
			}
			client, err := loadAlerts(cmd.Context(), loader, parent, args[0])
			if err != nil {
				return err
			}
			switch verb {
			case "list":
				result, err := client.List(cmd.Context(), adapter.ListOptions{})
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			case "get":
				result, err := client.Get(cmd.Context(), args[1])
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			case "create", "update":
				var result *Alert
				if verb == "create" {
					result, err = client.Create(cmd.Context(), &item)
				} else {
					result, err = client.Update(cmd.Context(), args[1], &item)
				}
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation(verb+"d", cmdio.MutationTarget{Kind: "Alert", Name: result.ID}))
			case "delete":
				if err = client.Delete(cmd.Context(), args[1]); err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "Alert", Name: args[1]}))
			}
			return nil
		}
		registerReadTable(&opts.IO, verb, cmdio.Table[Alert]{Columns: []cmdio.Column[Alert]{
			{Header: "ID", Content: func(o Alert) string { return o.ID }},
			{Header: "TITLE", Content: func(o Alert) string { return o.Title }},
		}})
		opts.setup(cmd.Flags(), verb)
		group.AddCommand(cmd)
	}
}

func loadAlerts(ctx context.Context, loader providers.GrafanaConfigLoader, parent, id string) (*client[Alert], error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("create Grafana HTTP client: %w", err)
	}
	return &client[Alert]{deps: adapter.ClientDeps{HTTP: httpClient, BaseURL: cfg.Host, Namespace: cfg.Namespace}, path: parent + "/" + id + "/alerts"}, nil
}

func registerReadTable[T any](opts *cmdio.Options, verb string, table cmdio.Table[T]) {
	if verb != "list" && verb != "get" {
		return
	}
	cmdio.RegisterTable(opts, table)
	if verb == "get" {
		single := cmdio.TableAdapter[*T, T]{Table: table, Rows: func(o *T) ([]T, error) { return []T{*o}, nil }}
		opts.RegisterCustomCodec("table", single.Codec("table"))
	}
	opts.DefaultFormat("table")
}
