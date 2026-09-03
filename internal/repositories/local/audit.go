package local

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type AuditRepo struct{ s *Store }

func NewAuditRepo(store *Store) *AuditRepo { return &AuditRepo{s: store} }

func (r *AuditRepo) AppendAudit(ctx context.Context, event entities.AuditEvent) error {
	if event.SafeMetadata == nil {
		event.SafeMetadata = map[string]string{}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = r.s.DB.ExecContext(ctx, `INSERT INTO audit_events(id,ts,payload) VALUES(?,?,?)`, event.ID, event.TS.UTC().Format(time.RFC3339Nano), payload)
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
	_ = json.Unmarshal(payload, &cursor)
	return cursor
}

func (r *AuditRepo) QueryAudit(ctx context.Context, query entities.AuditQuery) (*entities.AuditPage, error) {
	rows, err := r.s.DB.QueryContext(ctx, `SELECT payload FROM audit_events ORDER BY ts DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cursor := decodeAuditCursor(query.Cursor)
	values := make([]entities.AuditEvent, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var event entities.AuditEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, err
		}
		if query.Visibility.PrincipalType != entities.PrincipalMaster && event.OrganizationID != query.Visibility.OrganizationID ||
			query.OrganizationID != "" && event.OrganizationID != query.OrganizationID ||
			query.Since != nil && event.TS.Before(*query.Since) || query.Until != nil && event.TS.After(*query.Until) ||
			query.ActorID != "" && event.ActorID != query.ActorID || query.Action != "" && event.Action != query.Action ||
			query.TargetType != "" && event.TargetType != query.TargetType || query.TargetID != "" && event.TargetID != query.TargetID ||
			!cursor.TS.IsZero() && (event.TS.After(cursor.TS) || event.TS.Equal(cursor.TS) && event.ID >= cursor.ID) {
			continue
		}
		values = append(values, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].TS.After(values[j].TS) || values[i].TS.Equal(values[j].TS) && values[i].ID > values[j].ID
	})
	limit := boundedConfigLimit(query.Limit)
	page := &entities.AuditPage{Data: values}
	if len(page.Data) > limit {
		page.NextCursor = encodeAuditCursor(page.Data[limit-1])
		page.Data = page.Data[:limit]
	}
	return page, nil
}
