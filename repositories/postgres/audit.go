package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type AuditRepo struct{ db *DB }

func NewAuditRepo(db *DB) *AuditRepo { return &AuditRepo{db: db} }

func (r *AuditRepo) AppendAudit(ctx context.Context, event entities.AuditEvent) error {
	metadata, err := json.Marshal(event.SafeMetadata)
	if err != nil {
		return err
	}
	_, err = r.db.Pool.Exec(ctx, `INSERT INTO audit_events (id,ts,actor_type,actor_id,actor_label,organization_id,action,target_type,target_id,safe_metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (id) DO NOTHING`, event.ID, event.TS, event.ActorType, event.ActorID, event.ActorLabel, event.OrganizationID, event.Action, event.TargetType, event.TargetID, metadata)
	return err
}

type auditCursor struct {
	TS time.Time `json:"t"`
	ID string    `json:"i"`
}

func encodeAuditCursor(event entities.AuditEvent) string {
	payload, _ := json.Marshal(auditCursor{TS: event.TS, ID: event.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeAuditCursor(value string) auditCursor {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return auditCursor{}
	}
	var cursor auditCursor
	if json.Unmarshal(payload, &cursor) != nil {
		return auditCursor{}
	}
	return cursor
}

func (r *AuditRepo) QueryAudit(ctx context.Context, query entities.AuditQuery) (*entities.AuditPage, error) {
	limit := boundedLimit(query.Limit)
	cursor := decodeAuditCursor(query.Cursor)
	var since, until time.Time
	if query.Since != nil {
		since = *query.Since
	}
	if query.Until != nil {
		until = *query.Until
	}
	master := query.Visibility.PrincipalType == entities.PrincipalMaster
	rows, err := r.db.Pool.Query(ctx, `SELECT id,ts,actor_type,actor_id,actor_label,organization_id,action,target_type,target_id,safe_metadata
		FROM audit_events WHERE
		($1 OR organization_id=$2) AND
		($3::timestamptz='0001-01-01 00:00:00+00' OR ts >= $3) AND
		($4::timestamptz='0001-01-01 00:00:00+00' OR ts <= $4) AND
		($5::timestamptz='0001-01-01 00:00:00+00' OR (ts,id) < ($5,$6)) AND
		($7='' OR actor_id=$7) AND ($8='' OR action=$8) AND
		($9='' OR target_type=$9) AND ($10='' OR target_id=$10)
		ORDER BY ts DESC,id DESC LIMIT $11`, master, query.Visibility.OrganizationID, since, until, cursor.TS, cursor.ID, query.ActorID, query.Action, query.TargetType, query.TargetID, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &entities.AuditPage{Data: make([]entities.AuditEvent, 0, limit)}
	for rows.Next() {
		var event entities.AuditEvent
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.TS, &event.ActorType, &event.ActorID, &event.ActorLabel, &event.OrganizationID, &event.Action, &event.TargetType, &event.TargetID, &metadata); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &event.SafeMetadata); err != nil {
			return nil, err
		}
		page.Data = append(page.Data, event)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if len(page.Data) > limit {
		page.NextCursor = encodeAuditCursor(page.Data[limit-1])
		page.Data = page.Data[:limit]
	}
	return page, nil
}
