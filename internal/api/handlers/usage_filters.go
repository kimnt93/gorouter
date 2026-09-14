package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/policy"
)

// readUsageSelection accepts CSV and repeated parameters. OR within each
// dimension, AND between dimensions. An omitted/empty dimension selects all
// values inside the independently computed authorization boundary.
func readUsageSelection(c fiber.Ctx, name string) (string, error) {
	values := c.Request().URI().QueryArgs().PeekMulti(name)
	if len(values) > 100 {
		return "", fmt.Errorf("%s allows at most 100 values", name)
	}
	var selected []string
	seen := map[string]bool{}
	count := 0
	for _, raw := range values {
		if len(raw) > 12900 {
			return "", fmt.Errorf("%s is too long", name)
		}
		for _, part := range strings.Split(string(raw), ",") {
			count++
			if count > 100 {
				return "", fmt.Errorf("%s allows at most 100 values", name)
			}
			value := strings.TrimSpace(part)
			if value == "" {
				if len(raw) != 0 {
					return "", fmt.Errorf("%s contains an empty selection", name)
				}
				continue
			}
			if len(value) > 128 {
				return "", fmt.Errorf("%s values must not exceed 128 bytes", name)
			}
			for _, r := range value {
				if !(r == '-' || r == '_' || r == '.' || r == ':' || r == '/' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
					return "", fmt.Errorf("%s contains invalid characters", name)
				}
			}
			if !seen[value] {
				selected = append(selected, value)
				seen[value] = true
			}
		}
	}
	sort.Strings(selected)
	return strings.Join(selected, ","), nil
}

func applyUsageFilters(c fiber.Ctx, q *entities.UsageQuery) error {
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"organization_id", &q.OrganizationID}, {"user_id", &q.UserID},
		{"model", &q.Model}, {"api_key_id", &q.APIKeyID}, {"provider", &q.Provider},
		{"credential_id", &q.CredentialID}, {"application", &q.Application},
		{"environment", &q.Environment}, {"workspace_id", &q.WorkspaceID},
		{"agent_id", &q.AgentID}, {"conversation_id", &q.ConversationID},
		{"run_id", &q.RunID}, {"parent_run_id", &q.ParentRunID},
		{"trace_id", &q.TraceID}, {"logical_request_id", &q.LogicalRequestID},
	} {
		value, err := readUsageSelection(c, field.name)
		if err != nil {
			return err
		}
		*field.target = value
	}
	if cursor := c.Query("cursor"); cursor != "" {
		if len(cursor) > 2048 {
			return errors.New("cursor is invalid")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		var value struct {
			TS time.Time `json:"t"`
			ID string    `json:"i"`
		}
		if err != nil || json.Unmarshal(decoded, &value) != nil || value.TS.IsZero() || value.ID == "" {
			return errors.New("cursor is invalid")
		}
	}
	return nil
}

// usageReadVisibility resolves context before any repository aggregation or
// pagination. Selection fields never become authorization fields.
func (a *Admin) usageReadVisibility(c fiber.Ctx) (entities.UsageVisibility, error) {
	actor, err := a.principalForRead(c)
	if err != nil {
		return entities.UsageVisibility{}, err
	}
	organization, err := readUsageSelection(c, "organization_id")
	if err != nil {
		return entities.UsageVisibility{}, err
	}
	if actor.Type != entities.PrincipalMaster && strings.Contains(organization, ",") {
		return entities.UsageVisibility{}, errors.New("multiple organization filters require the master session")
	}
	if actor.Type == entities.PrincipalUser && organization != "" && actor.OrganizationID == "" {
		if a.IdentityRepo == nil {
			return entities.UsageVisibility{}, policy.ErrForbidden
		}
		membership, memberErr := a.IdentityRepo.Membership(c.Context(), organization, actor.UserID)
		org, orgErr := a.IdentityRepo.OrganizationByID(c.Context(), organization)
		if memberErr != nil || orgErr != nil || org == nil || org.Status != entities.StatusActive {
			return entities.UsageVisibility{}, policy.ErrForbidden
		}
		actor.OrganizationID, actor.MembershipRole = organization, membership.Role
	}
	if organization != "" && !strings.Contains(organization, ",") {
		if actor.OrganizationID != "" && actor.OrganizationID != organization {
			return entities.UsageVisibility{}, policy.ErrForbidden
		}
		if actor.Type == entities.PrincipalMaster {
			actor.OrganizationID = organization
		}
	}
	wide := actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin
	v, err := policy.UsageVisibility(actor, wide)
	if err != nil {
		return v, err
	}

	return v, nil
}

var errUsageUnavailable = errors.New("usage service unavailable")

func applyUsageTimes(c fiber.Ctx, q *entities.UsageQuery) error {
	for _, field := range []struct {
		name   string
		target **time.Time
	}{{"since", &q.Since}, {"until", &q.Until}} {
		if value := c.Query(field.name); value != "" {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return fmt.Errorf("%s must be RFC3339", field.name)
			}
			utc := parsed.UTC()
			*field.target = &utc
		}
	}
	if q.Since != nil && q.Until != nil && !q.Since.Before(*q.Until) {
		return errors.New("since must be before until")
	}
	return nil
}

func usageReadFailure(c fiber.Ctx, err error) error {
	if errors.Is(err, errUsageUnavailable) {
		return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "usage service unavailable", "service_unavailable", "usage_unavailable").Send()
	}
	if errors.Is(err, policy.ErrForbidden) {
		return responseapi.For(c).Forbidden("usage access is not allowed").Send()
	}
	if errors.Is(err, entities.ErrNotFound) {
		return responseapi.For(c).NotFound("usage context not found").Send()
	}
	return responseapi.For(c).BadRequest(err.Error()).Send()
}
