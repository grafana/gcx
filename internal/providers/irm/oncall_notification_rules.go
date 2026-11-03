package irm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PersonalNotificationRule is a user notification policy from the IRM internal API.
// The public OnCall API exposes the same model as personal_notification_rules.
//
//nolint:recvcheck // TypedCRUD requires a value getter and a pointer setter for resource identity.
type PersonalNotificationRule struct {
	ID           string          `json:"id,omitempty"`
	User         string          `json:"user,omitempty"`
	Important    bool            `json:"important"`
	Step         int             `json:"step"`
	NotifyBy     *int            `json:"notify_by"`
	WaitDelay    *string         `json:"wait_delay"`
	NotifyDevice json.RawMessage `json:"notify_device,omitempty"`
	Order        int             `json:"order,omitempty"`
}

func (r PersonalNotificationRule) GetResourceName() string      { return r.ID }
func (r *PersonalNotificationRule) SetResourceName(name string) { r.ID = name }

// ListPersonalNotificationRules returns all users' rules at both priority levels.
func (c *OnCallClient) ListPersonalNotificationRules(ctx context.Context) ([]PersonalNotificationRule, error) {
	// Absent parameters default to the caller and important=false. Empty values
	// disable both defaults and leave the Django filters inactive.
	return collectAll(iterResources[PersonalNotificationRule](ctx, c, pathWithParams(notificationPoliciesPath, url.Values{"user": {""}, "important": {""}}), "personal notification rule"))
}

func (c *OnCallClient) GetPersonalNotificationRule(ctx context.Context, id string) (*PersonalNotificationRule, error) {
	return getResourceWithNotFound[PersonalNotificationRule](ctx, c, notificationPoliciesPath, id, "personal notification rule",
		fmt.Errorf("irm: personal notification rule %q %w", id, adapter.ErrNotFound))
}
func (c *OnCallClient) CreatePersonalNotificationRule(ctx context.Context, r PersonalNotificationRule) (*PersonalNotificationRule, error) {
	return createResource[PersonalNotificationRule, PersonalNotificationRule](ctx, c, notificationPoliciesPath, r, "personal notification rule")
}
func (c *OnCallClient) UpdatePersonalNotificationRule(ctx context.Context, id string, r PersonalNotificationRule) (*PersonalNotificationRule, error) {
	return updateResource[PersonalNotificationRule, PersonalNotificationRule](ctx, c, notificationPoliciesPath, id, r, "personal notification rule")
}
func (c *OnCallClient) DeletePersonalNotificationRule(ctx context.Context, id string) error {
	return deleteResource(ctx, c, notificationPoliciesPath, id, "personal notification rule")
}
func listPersonalNotificationRules(ctx context.Context, c OnCallAPI) ([]PersonalNotificationRule, error) {
	return c.ListPersonalNotificationRules(ctx)
}
func getPersonalNotificationRule(ctx context.Context, c OnCallAPI, id string) (*PersonalNotificationRule, error) {
	return c.GetPersonalNotificationRule(ctx, id)
}
func personalNotificationRuleCRUDOpts() []crudOption[PersonalNotificationRule] {
	return []crudOption[PersonalNotificationRule]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, r *PersonalNotificationRule) (*PersonalNotificationRule, error) {
			return c.CreatePersonalNotificationRule(ctx, *r)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, id string, r *PersonalNotificationRule) (*PersonalNotificationRule, error) {
			return c.UpdatePersonalNotificationRule(ctx, id, *r)
		}),
		adapter.WithDelete[PersonalNotificationRule](func(ctx context.Context, c OnCallAPI, id string) error {
			return c.DeletePersonalNotificationRule(ctx, id)
		}),
	}
}
func newPersonalNotificationRulesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "personal-notification-rules", Aliases: []string{"personal-notification-rule", "pnr"}, Short: "Manage personal notification rules.", Long: `Manage personal notification rules through the IRM plugin proxy.
Use a Grafana user login (gcx login); service accounts cannot use this API.
List includes all users and both priority levels.
The server checks user settings read, write, and admin permissions.`}
	cmd.AddCommand(
		newListSubcommand(loader, "personal-notification-rules", "PersonalNotificationRule", "List personal notification rules for all users.", "id", listPersonalNotificationRules, getPersonalNotificationRule),
		newGetSubcommand(loader, "Get a personal notification rule by ID.", getPersonalNotificationRule),
		newCreateSubcommand(loader, "Create a personal notification rule.", personalNotificationRuleCRUDOpts()),
		newUpdateSubcommand(loader, "Update a personal notification rule by ID.", personalNotificationRuleCRUDOpts()),
		newDeleteSubcommand(loader, "Delete a personal notification rule by ID.", "PersonalNotificationRule", "personal notification rule", personalNotificationRuleCRUDOpts()),
	)
	return cmd
}
func personalNotificationRuleTable() cmdio.Table[unstructured.Unstructured] {
	return cmdio.Table[unstructured.Unstructured]{Columns: []cmdio.Column[unstructured.Unstructured]{
		{Header: "ID", Content: func(o unstructured.Unstructured) string { return o.GetName() }},
		{Header: "USER", Content: func(o unstructured.Unstructured) string { return orDash(specStr(o, "user")) }},
		{Header: "IMPORTANT", Content: func(o unstructured.Unstructured) string { return strconv.FormatBool(specBool(o, "important")) }},
		{Header: "STEP", Content: func(o unstructured.Unstructured) string { return orDash(specStr(o, "step")) }},
		{Header: "WAIT-DELAY", Content: func(o unstructured.Unstructured) string { return orDash(specStr(o, "wait_delay")) }},
		{Header: "NOTIFY-BY", Content: func(o unstructured.Unstructured) string { return orDash(specStr(o, "notify_by")) }},
	}}
}
