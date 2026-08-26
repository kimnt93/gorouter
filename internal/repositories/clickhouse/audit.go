package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type AuditRepo struct{ s *Store }

func NewAuditRepo(store *Store) *AuditRepo { return &AuditRepo{s: store} }

func (r *AuditRepo) AppendAudit(ctx context.Context, event entities.AuditEvent) error {
	if event.SafeMetadata == nil {
		event.SafeMetadata = map[string]string{}
	}
	metadata, err := json.Marshal(event.SafeMetadata)
	if err != nil {
		return err
	}
	return r.s.Conn.Exec(ctx, `INSERT INTO audit_events (id,ts,actor_type,actor_id,actor_label,organization_id,action,target_type,target_id,safe_metadata) VALUES (?,?,?,?,?,?,?,?,?,?)`, event.ID, event.TS, event.ActorType, event.ActorID, event.ActorLabel, event.OrganizationID, event.Action, event.TargetType, event.TargetID, string(metadata))
}

type clickhouseAuditCursor struct {
	TS time.Time `json:"t"`
	ID string    `json:"i"`
}

func clickhouseAuditCursorEncode(event entities.AuditEvent) string {
	payload, _ := json.Marshal(clickhouseAuditCursor{TS: event.TS, ID: event.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}
func clickhouseAuditCursorDecode(value string) clickhouseAuditCursor {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return clickhouseAuditCursor{}
	}
	var cursor clickhouseAuditCursor
	_ = json.Unmarshal(payload, &cursor)
	return cursor
}

func (r *AuditRepo) QueryAudit(ctx context.Context, query entities.AuditQuery) (*entities.AuditPage, error) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 12)
	if query.Visibility.PrincipalType != entities.PrincipalMaster {
		clauses = append(clauses, "organization_id=?")
		args = append(args, query.Visibility.OrganizationID)
	}
	if query.OrganizationID != "" {
		clauses = append(clauses, "organization_id=?")
		args = append(args, query.OrganizationID)
	}
	if query.Since != nil {
		clauses = append(clauses, "ts>=?")
		args = append(args, *query.Since)
	}
	if query.Until != nil {
		clauses = append(clauses, "ts<=?")
		args = append(args, *query.Until)
	}
	cursor := clickhouseAuditCursorDecode(query.Cursor)
	if !cursor.TS.IsZero() {
		clauses = append(clauses, "(ts,id)<(?,?)")
		args = append(args, cursor.TS, cursor.ID)
	}
	for _, filter := range []struct {
		column string
		value  string
	}{{"actor_id", query.ActorID}, {"action", query.Action}, {"target_type", query.TargetType}, {"target_id", query.TargetID}} {
		if filter.value != "" {
			clauses = append(clauses, filter.column+"=?")
			args = append(args, filter.value)
		}
	}
	limit := boundedConfigLimit(query.Limit)
	args = append(args, limit+1)
	rows, err := r.s.Conn.Query(ctx, `SELECT id,ts,actor_type,actor_id,actor_label,organization_id,action,target_type,target_id,safe_metadata FROM audit_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY ts DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &entities.AuditPage{Data: make([]entities.AuditEvent, 0, limit)}
	seen := make(map[string]struct{}, limit+1)
	for rows.Next() {
		var event entities.AuditEvent
		var metadata string
		if err := rows.Scan(&event.ID, &event.TS, &event.ActorType, &event.ActorID, &event.ActorLabel, &event.OrganizationID, &event.Action, &event.TargetType, &event.TargetID, &metadata); err != nil {
			return nil, err
		}
		if _, duplicate := seen[event.ID]; duplicate {
			continue
		}
		seen[event.ID] = struct{}{}
		if err := json.Unmarshal([]byte(metadata), &event.SafeMetadata); err != nil {
			return nil, err
		}
		if event.SafeMetadata == nil {
			event.SafeMetadata = map[string]string{}
		}
		page.Data = append(page.Data, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Data) > limit {
		page.NextCursor = clickhouseAuditCursorEncode(page.Data[limit-1])
		page.Data = page.Data[:limit]
	}
	return page, nil
}
