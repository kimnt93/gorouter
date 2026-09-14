package entities

import "time"

// OrganizationModel publishes a one-to-one alias or a bulk-assignment package.
// SourceOwnerID is server-derived when publishing a personal provider source.
type OrganizationModel struct {
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	Targets        []string  `json:"targets"`
	SourceOwnerID  string    `json:"source_owner_id,omitempty"`
	Enabled        bool      `json:"enabled"`
	WeeklyLimitUSD *float64  `json:"weekly_limit_usd"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type OrganizationModelGrant struct {
	OrganizationID string    `json:"organization_id"`
	Model          string    `json:"model"`
	UserID         string    `json:"user_id"`
	Enabled        bool      `json:"enabled"`
	WeeklyLimitUSD *float64  `json:"weekly_limit_usd"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ModelBudgetCharge struct {
	Scope    string  `json:"scope"`
	LimitUSD float64 `json:"limit_usd"`
}

// ModelBudgetReservation is durable before provider work. Pending/unknown work
// keeps its estimate reserved across restart until a known settlement arrives.
type ModelBudgetReservation struct {
	ID             string              `json:"id"`
	OrganizationID string              `json:"organization_id"`
	UserID         string              `json:"user_id"`
	Model          string              `json:"model"`
	WindowStart    time.Time           `json:"window_start"`
	WindowEnd      time.Time           `json:"window_end"`
	Charges        []ModelBudgetCharge `json:"charges"`
	AmountUSD      float64             `json:"amount_usd"`
	Settled        bool                `json:"settled"`
	CreatedAt      time.Time           `json:"created_at"`
}
