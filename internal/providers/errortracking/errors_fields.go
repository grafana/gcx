package errortracking

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type errorFieldValue struct {
	Value      string `json:"value"`
	EventCount int64  `json:"eventCount"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
}

type errorField struct {
	Key        string                `json:"key"`
	ValueCount int64                 `json:"valueCount"`
	EventCount int64                 `json:"eventCount"`
	Saturated  bool                  `json:"saturated"`
	Values     list[errorFieldValue] `json:"values"`
}

type errorsListFieldsOpts struct{ errorTarget }

func (o *errorsListFieldsOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindListTable(&o.IO, flags, cmdio.Table[errorField]{Columns: []cmdio.Column[errorField]{
		{Header: "KEY", Content: func(f errorField) string { return f.Key }},
		{Header: "VALUES", Content: func(f errorField) string { return strconv.FormatInt(f.ValueCount, 10) }},
		{Header: "EVENTS", Content: func(f errorField) string { return strconv.FormatInt(f.EventCount, 10) }},
		{Header: "SATURATED", Content: func(f errorField) string { return strconv.FormatBool(f.Saturated) }},
		{Header: "TOP VALUES", Content: func(f errorField) string {
			top := make([]string, 0, 3)
			for _, v := range f.Values[:min(3, len(f.Values))] {
				top = append(top, oneLine(v.Value, 30))
			}
			return cmdio.OrDash(strings.Join(top, ", "))
		}},
	}})
}

func newErrorsListFieldsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsListFieldsOpts{}
	cmd := &cobra.Command{
		Use:   "list <error>",
		Short: "List the custom fields of an error's events, with their values.",
		Long: `List the custom fields (tags and contexts the SDK sent) of one error's
events: each key, its distinct values and events, and the values with their
event counts and first and last times.

SATURATED means the error hit the API's cap of distinct values for the key,
so its values may be incomplete. The API returns every key in one response,
so there is no --limit. <error> is the error ID from errors list.`,
		Example: `  # The custom fields of error 42.
  gcx error-tracking errors fields list 42 --project 314

  # The values of each key, as JSON.
  gcx et errors fields list 42 --project checkout-api --json key,values

  # As an agent: keys, value counts and whether they hit the cap.
  gcx error-tracking errors fields list 42 --project 314 --json key,valueCount,saturated`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args[0]); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, path, err := opts.connect(ctx, loader, nil)
			if err != nil {
				return err
			}
			var out struct {
				CustomFields []errorField `json:"customFields"`
			}
			if err := c.do(ctx, http.MethodGet, path+"/customFields", nil, nil, &out); err != nil {
				return err
			}
			if err := encodeList(cmd, c, &opts.IO, out.CustomFields, nil); err != nil {
				return err
			}
			if len(out.CustomFields) == 0 && !opts.IO.JSONDiscovery {
				say(cmd.ErrOrStderr(), "No custom fields on "+opts.shown(c), "")
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<error> --project 314 --json key,valueCount,saturated"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
