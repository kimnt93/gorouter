package entities

import "strings"

// UsageFilter is an exact-match OR set. Separate filters are always ANDed,
// including a single-agent filter combined with an authorized agent batch.
type UsageFilter struct {
	Field  string
	Values []string
}

// Filters provides identical selection semantics to all durable repositories.
// Empty filters mean all authorized records, never all principals.
func (q UsageQuery) Filters() []UsageFilter {
	out := make([]UsageFilter, 0, 16)
	for _, field := range []struct{ name, value string }{
		{"organization_id", q.OrganizationID}, {"user_id", q.UserID},
		{"model", q.Model}, {"api_key_id", q.APIKeyID}, {"provider", q.Provider},
		{"credential_id", q.CredentialID}, {"application", q.Application},
		{"environment", q.Environment}, {"workspace_id", q.WorkspaceID},
		{"agent_id", q.AgentID}, {"conversation_id", q.ConversationID},
		{"run_id", q.RunID}, {"parent_run_id", q.ParentRunID},
		{"logical_request_id", q.LogicalRequestID}, {"trace_id", q.TraceID},
	} {
		if field.value != "" {
			out = append(out, UsageFilter{Field: field.name, Values: strings.Split(field.value, ",")})
		}
	}
	if len(q.AgentIDs) > 0 {
		out = append(out, UsageFilter{Field: "agent_id", Values: q.AgentIDs})
	}
	binding := q.Workload
	if q.Visibility.Workload != nil {
		binding = q.Visibility.Workload
	}
	if binding != nil {
		for _, field := range []struct{ name, value string }{
			{"application", binding.Application}, {"environment", binding.Environment},
			{"workspace_id", binding.WorkspaceID}, {"agent_id", binding.AgentID},
		} {
			// Empty environment is an exact namespace, not a wildcard.
			out = append(out, UsageFilter{Field: field.name, Values: []string{field.value}})
		}
	}
	return out
}

// Allows checks immutable actor snapshots, independently of caller filters.
func (v UsageVisibility) Allows(userID, organizationID string) bool {
	if v.PrincipalType == PrincipalMaster {
		return v.OrganizationID == "" || v.OrganizationID == organizationID
	}
	if v.OrganizationWide {
		return v.OrganizationID != "" && v.OrganizationID == organizationID
	}
	return v.UserID != "" && v.UserID == userID && ((v.OrganizationID == "" && v.Workload == nil) || v.OrganizationID == organizationID)
}
