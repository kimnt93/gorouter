package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type IdentityRepo struct{ db *DB }

func NewIdentityRepo(db *DB) *IdentityRepo { return &IdentityRepo{db: db} }

func (r *IdentityRepo) CreateUser(ctx context.Context, user entities.User) error {
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO users (id,username,normalized_username,status,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6)`, user.ID, user.Username, user.NormalizedUsername, user.Status, user.CreatedAt, user.UpdatedAt)
	return err
}

func scanUser(row pgx.Row) (*entities.User, error) {
	var user entities.User
	if err := row.Scan(&user.ID, &user.Username, &user.NormalizedUsername, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *IdentityRepo) UserByID(ctx context.Context, id string) (*entities.User, error) {
	return scanUser(r.db.Pool.QueryRow(ctx, `SELECT id,username,normalized_username,status,created_at,updated_at FROM users WHERE id=$1`, id))
}

func (r *IdentityRepo) UserByNormalizedUsername(ctx context.Context, normalized string) (*entities.User, error) {
	return scanUser(r.db.Pool.QueryRow(ctx, `SELECT id,username,normalized_username,status,created_at,updated_at FROM users WHERE normalized_username=$1`, normalized))
}

func (r *IdentityRepo) ListUsers(ctx context.Context, query entities.PageQuery) ([]entities.User, string, error) {
	limit := boundedLimit(query.Limit)
	cursor := decodeIDCursor(query.Cursor)
	rows, err := r.db.Pool.Query(ctx, `SELECT id,username,normalized_username,status,created_at,updated_at FROM users
		WHERE ($1='' OR id < $1) AND ($2='' OR normalized_username LIKE '%' || lower($2) || '%') AND ($3='' OR status=$3)
		ORDER BY id DESC LIMIT $4`, cursor, strings.TrimSpace(query.Query), query.Status, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]entities.User, 0, limit)
	for rows.Next() {
		var user entities.User
		if err := rows.Scan(&user.ID, &user.Username, &user.NormalizedUsername, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, user)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	var next string
	if len(out) > limit {
		next = encodeIDCursor(out[limit-1].ID)
		out = out[:limit]
	}
	return out, next, nil
}

func (r *IdentityRepo) UpdateUserStatus(ctx context.Context, id, status string, updatedAt time.Time) error {
	tag, err := r.db.Pool.Exec(ctx, `UPDATE users SET status=$1,updated_at=$2 WHERE id=$3`, status, updatedAt, id)
	if err == nil && tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return err
}

func (r *IdentityRepo) CreateOrganization(ctx context.Context, organization entities.Organization) error {
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO organizations (id,name,normalized_name,status,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6)`, organization.ID, organization.Name, organization.NormalizedName, organization.Status, organization.CreatedAt, organization.UpdatedAt)
	return err
}

func scanOrganization(row pgx.Row) (*entities.Organization, error) {
	var organization entities.Organization
	if err := row.Scan(&organization.ID, &organization.Name, &organization.NormalizedName, &organization.Status, &organization.CreatedAt, &organization.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, err
	}
	return &organization, nil
}

func (r *IdentityRepo) OrganizationByID(ctx context.Context, id string) (*entities.Organization, error) {
	return scanOrganization(r.db.Pool.QueryRow(ctx, `SELECT id,name,normalized_name,status,created_at,updated_at FROM organizations WHERE id=$1`, id))
}

func (r *IdentityRepo) OrganizationByNormalizedName(ctx context.Context, normalized string) (*entities.Organization, error) {
	return scanOrganization(r.db.Pool.QueryRow(ctx, `SELECT id,name,normalized_name,status,created_at,updated_at FROM organizations WHERE normalized_name=$1`, normalized))
}

func (r *IdentityRepo) ListOrganizations(ctx context.Context, query entities.PageQuery) ([]entities.Organization, string, error) {
	limit := boundedLimit(query.Limit)
	rows, err := r.db.Pool.Query(ctx, `SELECT id,name,normalized_name,status,created_at,updated_at FROM organizations
		WHERE ($1='' OR id < $1) AND ($2='' OR normalized_name LIKE '%' || lower($2) || '%') AND ($3='' OR status=$3)
		ORDER BY id DESC LIMIT $4`, decodeIDCursor(query.Cursor), strings.TrimSpace(query.Query), query.Status, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]entities.Organization, 0, limit)
	for rows.Next() {
		var organization entities.Organization
		if err := rows.Scan(&organization.ID, &organization.Name, &organization.NormalizedName, &organization.Status, &organization.CreatedAt, &organization.UpdatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, organization)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	var next string
	if len(out) > limit {
		next = encodeIDCursor(out[limit-1].ID)
		out = out[:limit]
	}
	return out, next, nil
}

func (r *IdentityRepo) UpdateOrganization(ctx context.Context, organization entities.Organization) error {
	tag, err := r.db.Pool.Exec(ctx, `UPDATE organizations SET name=$1,normalized_name=$2,status=$3,updated_at=$4 WHERE id=$5`, organization.Name, organization.NormalizedName, organization.Status, organization.UpdatedAt, organization.ID)
	if err == nil && tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return err
}

func (r *IdentityRepo) CreateMembership(ctx context.Context, membership entities.Membership) error {
	result, err := r.db.Pool.Exec(ctx, `INSERT INTO organization_memberships (organization_id,user_id,role,created_at,created_by_actor_type,created_by_actor_id)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (organization_id,user_id) DO NOTHING`, membership.OrganizationID, membership.UserID, membership.Role, membership.CreatedAt, membership.CreatedByActorType, membership.CreatedByActorID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return entities.ErrConflict
	}
	return nil
}

func (r *IdentityRepo) PutMembership(ctx context.Context, membership entities.Membership) error {
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO organization_memberships (organization_id,user_id,role,created_at,created_by_actor_type,created_by_actor_id)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (organization_id,user_id) DO UPDATE SET role=EXCLUDED.role`, membership.OrganizationID, membership.UserID, membership.Role, membership.CreatedAt, membership.CreatedByActorType, membership.CreatedByActorID)
	return err
}

func scanMembership(row pgx.Row) (*entities.Membership, error) {
	var membership entities.Membership
	if err := row.Scan(&membership.OrganizationID, &membership.UserID, &membership.Role, &membership.CreatedAt, &membership.CreatedByActorType, &membership.CreatedByActorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, err
	}
	return &membership, nil
}

func (r *IdentityRepo) Membership(ctx context.Context, organizationID, userID string) (*entities.Membership, error) {
	return scanMembership(r.db.Pool.QueryRow(ctx, `SELECT organization_id,user_id,role,created_at,created_by_actor_type,created_by_actor_id FROM organization_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID))
}

func (r *IdentityRepo) ListMemberships(ctx context.Context, organizationID string) ([]entities.Membership, error) {
	return r.listMemberships(ctx, `SELECT organization_id,user_id,role,created_at,created_by_actor_type,created_by_actor_id FROM organization_memberships WHERE organization_id=$1 ORDER BY created_at,user_id`, organizationID)
}

func (r *IdentityRepo) ListMembershipsForUser(ctx context.Context, userID string) ([]entities.Membership, error) {
	return r.listMemberships(ctx, `SELECT organization_id,user_id,role,created_at,created_by_actor_type,created_by_actor_id FROM organization_memberships WHERE user_id=$1 ORDER BY created_at,organization_id`, userID)
}

func (r *IdentityRepo) listMemberships(ctx context.Context, sql string, id string) ([]entities.Membership, error) {
	rows, err := r.db.Pool.Query(ctx, sql, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []entities.Membership{}
	for rows.Next() {
		var membership entities.Membership
		if err := rows.Scan(&membership.OrganizationID, &membership.UserID, &membership.Role, &membership.CreatedAt, &membership.CreatedByActorType, &membership.CreatedByActorID); err != nil {
			return nil, err
		}
		out = append(out, membership)
	}
	return out, rows.Err()
}

func (r *IdentityRepo) CountActiveOrganizationAdmins(ctx context.Context, organizationID string) (int, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx, `SELECT count(*) FROM organization_memberships m JOIN users u ON u.id=m.user_id JOIN organizations o ON o.id=m.organization_id WHERE m.organization_id=$1 AND m.role='admin' AND u.status='active' AND o.status='active'`, organizationID).Scan(&count)
	return count, err
}

func (r *IdentityRepo) DeleteMembership(ctx context.Context, organizationID, userID string) error {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM organization_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return err
}

func (r *IdentityRepo) ChangeMembershipRoleAtomic(ctx context.Context, organizationID, userID, role string) (bool, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, organizationID); err != nil {
		return false, err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT role FROM organization_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
		return false, entities.ErrNotFound
	} else if err != nil {
		return false, err
	}
	if current == entities.MembershipAdmin && role != entities.MembershipAdmin {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1 AND m.role='admin' AND u.status='active'`, organizationID).Scan(&count); err != nil {
			return false, err
		}
		if count <= 1 {
			return true, nil
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE organization_memberships SET role=$1 WHERE organization_id=$2 AND user_id=$3`, role, organizationID, userID); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
func (r *IdentityRepo) DeleteMembershipAtomic(ctx context.Context, organizationID, userID string) (bool, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, organizationID); err != nil {
		return false, err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT role FROM organization_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
		return false, entities.ErrNotFound
	} else if err != nil {
		return false, err
	}
	if current == entities.MembershipAdmin {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships m JOIN users u ON u.id=m.user_id WHERE m.organization_id=$1 AND m.role='admin' AND u.status='active'`, organizationID).Scan(&count); err != nil {
			return false, err
		}
		if count <= 1 {
			return true, nil
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM organization_memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func encodeIDCursor(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }
func decodeIDCursor(cursor string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return ""
	}
	return string(decoded)
}
