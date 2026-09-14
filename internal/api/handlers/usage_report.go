package handlers

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/policy"
	"github.com/kimnt93/gorouter/pkg/quota"
)

// UsageReport returns bounded, SQL-aggregated stored usage without health/content.
// @Summary Get a bounded usage report
// @Description Single-statement stored usage snapshot. Coverage is unknown until durable acceptance tracking is available. Empty results do not establish measured free work. Empty dimension IDs denote unattributed history. Token totals exclude replayed Router response-cache metadata. Cache read ratio is cache_read_tokens/(input_tokens+cache_read_tokens), undefined when denominator is zero. Cost is historical Router-priced USD, not a provider invoice.
// @Tags usage
// @Security BearerAuth
// @Produce json
// @Param scope query string false "personal (user default), organization (context default), all_owned, global (master only)"
// @Param range query string false "24h (default), 7d, 30d; explicit bounds override presets"
// @Param since query string false "RFC3339 inclusive lower bound; maximum five years"
// @Param until query string false "RFC3339 exclusive upper bound"
// @Param time_basis query string false "accounting (default) or completion"
// @Param group_by query string false "agent, model, user; omitted means no groups; maximum 100"
// @Param series_by query string false "agent, model, user; omitted means combined series"
// @Param bucket query string false "hour, day (default), week, month; maximum 2000 buckets/20000 cells"
// @Param organization_id query string false "Authorized organization context"
// @Param user_id query string false "CSV/repeated IDs (100 maximum)"
// @Param agent_id query string false "CSV/repeated IDs (100 maximum)"
// @Param conversation_id query string false "CSV/repeated IDs (100 maximum)"
// @Param run_id query string false "CSV/repeated IDs (100 maximum)"
// @Param parent_run_id query string false "CSV/repeated IDs (100 maximum)"
// @Param trace_id query string false "CSV/repeated IDs (100 maximum)"
// @Param logical_request_id query string false "CSV/repeated IDs (100 maximum)"
// @Param model query string false "CSV/repeated model IDs"
// @Param provider query string false "CSV/repeated provider IDs"
// @Param api_key_id query string false "CSV/repeated key IDs"
// @Param credential_id query string false "CSV/repeated credential IDs"
// @Param application query string false "Legacy application filter"
// @Param environment query string false "Legacy environment filter"
// @Param workspace_id query string false "Legacy workspace filter"
// @Param view_user_id query string false "Master-only user View As"
// @Success 200 {object} entities.UsageReport
// @Failure 400,401,403,404,503 {object} responseapi.ErrorResponse
// @Router /admin/usage/report [get]
func (a *Admin) UsageReport(c fiber.Ctx) error {
	api := responseapi.For(c)
	c.Set(fiber.HeaderCacheControl, "no-store")
	v, err := a.usageReadVisibility(c)
	if err != nil {
		return usageReadFailure(c, err)
	}
	scope := c.Query("scope")
	if scope == "" {
		switch {
		case v.OrganizationID != "":
			scope = "organization"
		case v.PrincipalType == entities.PrincipalMaster:
			scope = "global"
		default:
			scope = "personal"
		}
	}
	q := entities.UsageReportQuery{UsageQuery: entities.UsageQuery{Visibility: v, TimeBasis: c.Query("time_basis", "accounting")}, GroupBy: c.Query("group_by"), SeriesBy: c.Query("series_by"), Bucket: c.Query("bucket", "day")}
	switch scope {
	case "personal":
		if v.OrganizationID != "" || v.OrganizationWide || v.UserID == "" {
			return usageReadFailure(c, policy.ErrForbidden)
		}
		q.PersonalOnly = true
	case "all_owned":
		if v.OrganizationID != "" || v.OrganizationWide || v.UserID == "" {
			return usageReadFailure(c, policy.ErrForbidden)
		}
	case "organization":
		if v.OrganizationID == "" {
			return api.BadRequest("organization scope requires authorized organization context").Send()
		}
	case "global":
		if v.PrincipalType != entities.PrincipalMaster || v.OrganizationID != "" {
			return usageReadFailure(c, policy.ErrForbidden)
		}
	default:
		return api.BadRequest("invalid report scope").Send()
	}
	if err = applyUsageFilters(c, &q.UsageQuery); err != nil {
		return api.BadRequest(err.Error()).Send()
	}
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	until := now
	switch c.Query("range", "24h") {
	case "24h":
	case "7d":
		since = now.AddDate(0, 0, -7)
	case "30d":
		since = now.AddDate(0, 0, -30)
	default:
		if c.Query("since") == "" || c.Query("until") == "" {
			return api.BadRequest("report requires a bounded range").Send()
		}
	}
	q.Since, q.Until = &since, &until
	if err = applyUsageTimes(c, &q.UsageQuery); err != nil {
		return api.BadRequest(err.Error()).Send()
	}
	week, _, _, _ := quota.Window("week", now)
	q.WeekStart = week.Weekday()
	if a.UsageSvc == nil {
		return usageReadFailure(c, errUsageUnavailable)
	}
	result, err := a.UsageSvc.Report(c.Context(), q, entities.UsageReportScope{Kind: scope, UserID: v.UserID, OrganizationID: v.OrganizationID})
	if errors.Is(err, entities.ErrUsageReportLimit) {
		return api.Error(400, "invalid report dimensions or report exceeds range/cardinality limits", "invalid_request_error", "usage_report_limit").Send()
	}
	if err != nil {
		return usageReadFailure(c, errUsageUnavailable)
	}
	return api.Response().Status(200).Data(result).Send()
}

// CapabilitiesResponse deliberately advertises supported behavior, not target
// release aspirations. Consumers must require individual capability flags.
type CapabilitiesResponse struct {
	Version      string                        `json:"version"`
	Backend      string                        `json:"backend"`
	Capabilities AccountingCapabilities        `json:"capabilities"`
	Measurement  AccountingMeasurementFeatures `json:"measurement"`
}
type AccountingCapabilities struct {
	UsageReport          string `json:"usage_report"`
	UserWeeklyUsage      string `json:"user_weekly_usage"`
	TotalsOnly           bool   `json:"totals_only"`
	CanonicalKeyMetadata bool   `json:"canonical_key_metadata"`
	DurableAcceptance    bool   `json:"durable_acceptance"`
	UsageReceipts        bool   `json:"usage_receipts"`
	MemberAllocations    bool   `json:"member_allocations"`
	AtomicCreditCounters bool   `json:"atomic_credit_counters"`
}
type AccountingMeasurementFeatures struct {
	TokenComponents    []string `json:"token_components"`
	MissingPricePolicy string   `json:"missing_price_policy"`
	Coverage           string   `json:"coverage"`
}

// Capabilities describes actual accounting features without key or content data.
// @Summary Get dependency capabilities
// @Tags usage
// @Security BearerAuth
// @Success 200 {object} CapabilitiesResponse
// @Failure 401 {object} responseapi.ErrorResponse
// @Router /admin/capabilities [get]
func (a *Admin) Capabilities(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	backend := a.DatabaseBackend
	if backend == "" {
		backend = "unknown"
	}
	caps := AccountingCapabilities{CanonicalKeyMetadata: a.KeysSvc != nil}
	if a.UsageSvc != nil {
		caps.TotalsOnly = true
		caps.UserWeeklyUsage = "gorouter-user-usage-v1"
		if a.UsageSvc.SupportsReport() {
			caps.UsageReport = "gorouter-usage-report-v1"
		}
	}
	return responseapi.For(c).Response().Status(200).Data(CapabilitiesResponse{Version: "0.2.2-dev", Backend: backend, Capabilities: caps, Measurement: AccountingMeasurementFeatures{TokenComponents: []string{"uncached_input", "cache_read", "cache_write", "output"}, MissingPricePolicy: "free_by_policy", Coverage: "stored_only"}}).Send()
}

// CanonicalKeyMetadataResponse intentionally has no key prefix, hash, ciphertext,
// plaintext or secret-derived revision. Revision covers public metadata only.
type CanonicalKeyMetadataResponse struct {
	KeyID    string   `json:"key_id"`
	OwnerID  string   `json:"owner_id"`
	Enabled  bool     `json:"enabled"`
	Scopes   []string `json:"scopes"`
	Revision string   `json:"revision"`
}

// CanonicalUserKey returns owner/master-only safe metadata, never a reveal.
// @Summary Get canonical user API key metadata
// @Tags keys
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} CanonicalKeyMetadataResponse
// @Failure 401,403,404,503 {object} responseapi.ErrorResponse
// @Router /admin/users/{id}/api-key [get]
func (a *Admin) CanonicalUserKey(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	api := responseapi.For(c)
	actor := principalFromSession(SessionFrom(c))
	id := strings.TrimSpace(c.Params("id"))
	if !actor.HasScope(entities.ScopeKeysManage) {
		return api.Forbidden("key metadata access is not allowed").Send()
	}
	if actor.OrganizationID != "" || c.Query("organization_id") != "" || c.Query("view_user_id") != "" && c.Query("view_user_id") != id || actor.Type != entities.PrincipalMaster && (actor.Type != entities.PrincipalUser || actor.UserID != id) {
		return api.NotFound("user key not found").Send()
	}
	if a.KeysSvc == nil {
		return api.Error(503, "key lookup unavailable", "service_unavailable", "key_unavailable").Send()
	}
	key, err := a.KeysSvc.PrimaryForUser(c.Context(), id)
	if errors.Is(err, entities.ErrNotFound) {
		return api.NotFound("user key not found").Send()
	}
	if err != nil {
		return api.Error(503, "key lookup unavailable", "service_unavailable", "key_unavailable").Send()
	}
	return api.Response().Status(200).Data(CanonicalKeyMetadataResponse{KeyID: key.ID, OwnerID: key.OwnerUserID, Enabled: key.Enabled, Scopes: append([]string{}, key.Scopes...), Revision: keyMetadataRevision(key)}).Send()
}
