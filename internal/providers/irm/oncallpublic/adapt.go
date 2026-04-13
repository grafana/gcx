package oncallpublic

import (
	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
)

func adaptIntegration(p integration) oncalltypes.Integration {
	return oncalltypes.Integration{
		ID:               p.ID,
		VerbalName:       p.Name,
		Integration:      p.Type,
		IntegrationURL:   p.Link,
		InboundEmail:     p.InboundEmail,
		DescriptionShort: p.DescriptionShort,
		Team:             p.TeamID,
		MaintenanceMode:  p.MaintenanceMode,
		Labels:           p.Labels,
	}
}

func adaptEscalationChain(p escalationChain) oncalltypes.EscalationChain {
	return oncalltypes.EscalationChain{
		ID:   p.ID,
		Name: p.Name,
		Team: p.TeamID,
	}
}

func adaptEscalationPolicy(p escalationPolicy) oncalltypes.EscalationPolicy {
	return oncalltypes.EscalationPolicy{
		ID:                 p.ID,
		Step:               p.Type,
		WaitDelay:          p.Duration,
		EscalationChain:    p.EscalationChainID,
		NotifyToUsersQueue: p.PersonsToNotify,
		NotifySchedule:     p.NotifyOnCallFromSchedule,
		NotifyToGroup:      p.GroupsToNotify,
		CustomWebhook:      p.ActionToTrigger,
		Important:          p.Important,
		Severity:           p.Severity,
	}
}

func adaptSchedule(p schedule) oncalltypes.Schedule {
	return oncalltypes.Schedule{
		ID:        p.ID,
		Name:      p.Name,
		Type:      p.Type,
		Team:      p.TeamID,
		TimeZone:  p.TimeZone,
		OnCallNow: p.OnCallNow,
	}
}

func adaptShift(p shift) oncalltypes.Shift {
	return oncalltypes.Shift{
		ID:            p.ID,
		Name:          p.Name,
		Type:          p.Type,
		ShiftStart:    p.Start,
		ShiftEnd:      p.Duration,
		PriorityLevel: p.Level,
		Frequency:     p.Frequency,
		Interval:      p.Interval,
		ByDay:         p.ByDay,
	}
}

func adaptRoute(p route) oncalltypes.Route {
	return oncalltypes.Route{
		ID:                  p.ID,
		AlertReceiveChannel: p.IntegrationID,
		EscalationChain:     p.EscalationChainID,
		FilteringTerm:       p.RoutingRegex,
		FilteringTermType:   p.RoutingType,
		IsDefault:           p.IsTheLastRoute,
	}
}

func adaptWebhook(p webhook) oncalltypes.Webhook {
	return oncalltypes.Webhook{
		ID:                  p.ID,
		Name:                p.Name,
		URL:                 p.URL,
		HTTPMethod:          p.HTTPMethod,
		TriggerType:         p.TriggerType,
		IsWebhookEnabled:    p.IsWebhookEnabled,
		Team:                p.TeamID,
		Data:                p.Data,
		Username:            p.Username,
		Password:            p.Password,
		AuthorizationHeader: p.AuthorizationHeader,
		Headers:             p.Headers,
		TriggerTemplate:     p.TriggerTemplate,
		IntegrationFilter:   p.IntegrationFilter,
		ForwardAll:          p.ForwardAll,
		Preset:              p.Preset,
	}
}

func adaptAlertGroup(p alertGroup) oncalltypes.AlertGroup {
	return oncalltypes.AlertGroup{
		PK:                  p.ID,
		Status:              p.State,
		StartedAt:           p.CreatedAt,
		ResolvedAt:          p.ResolvedAt,
		AcknowledgedAt:      p.AcknowledgedAt,
		SilencedAt:          p.SilencedAt,
		AlertsCount:         p.AlertsCount,
		AlertReceiveChannel: p.IntegrationID,
		Team:                p.TeamID,
		Labels:              p.Labels,
		RenderForWeb:        p.Title,
		Permalinks:          p.Permalinks,
	}
}

func adaptUser(p user) oncalltypes.User {
	return oncalltypes.User{
		PK:                p.ID,
		Username:          p.Username,
		Email:             p.Email,
		Name:              p.Name,
		Role:              p.Role,
		Avatar:            p.AvatarURL,
		Timezone:          p.Timezone,
		CurrentTeam:       p.Teams,
		SlackUserIdentity: p.Slack,
	}
}

func adaptTeam(p team) oncalltypes.Team {
	return oncalltypes.Team{
		ID:        p.ID,
		Name:      p.Name,
		Email:     p.Email,
		AvatarURL: p.AvatarURL,
	}
}

func adaptUserGroup(p userGroup) oncalltypes.UserGroup {
	return oncalltypes.UserGroup{
		ID:     p.ID,
		Name:   p.Name,
		Handle: p.Handle,
	}
}

func adaptSlackChannel(p slackChannel) oncalltypes.SlackChannel {
	return oncalltypes.SlackChannel{
		ID:          p.ID,
		DisplayName: p.Name,
		SlackID:     p.SlackID,
	}
}

func adaptAlert(p alert) oncalltypes.Alert {
	return oncalltypes.Alert{
		ID:                    p.ID,
		LinkToUpstreamDetails: p.Link,
		CreatedAt:             p.CreatedAt,
	}
}

func adaptOrganization(p organization) oncalltypes.Organization {
	return oncalltypes.Organization{
		PK:        p.ID,
		Name:      p.Name,
		StackSlug: p.Slug,
	}
}

func adaptResolutionNote(p resolutionNote) oncalltypes.ResolutionNote {
	var author any
	if p.Author != nil {
		author = *p.Author
	}
	return oncalltypes.ResolutionNote{
		ID:         p.ID,
		AlertGroup: p.AlertGroupID,
		Author:     author,
		Source:      p.Source,
		CreatedAt:  p.CreatedAt,
		Text:       p.Text,
	}
}

func adaptShiftSwap(p shiftSwap) oncalltypes.ShiftSwap {
	return oncalltypes.ShiftSwap{
		ID:          p.ID,
		Schedule:    p.Schedule,
		SwapStart:   p.SwapStart,
		SwapEnd:     p.SwapEnd,
		Beneficiary: p.Beneficiary,
		Benefactor:  p.Benefactor,
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
	}
}

func adaptSlice[Pub any, Int any](items []Pub, fn func(Pub) Int) []Int {
	result := make([]Int, len(items))
	for i, item := range items {
		result[i] = fn(item)
	}
	return result
}
