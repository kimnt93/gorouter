package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type memoryRepo struct {
	users             map[string]entities.User
	names             map[string]string
	organizations     map[string]entities.Organization
	organizationNames map[string]string
	memberships       map[string]entities.Membership
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{map[string]entities.User{}, map[string]string{}, map[string]entities.Organization{}, map[string]string{}, map[string]entities.Membership{}}
}
func (r *memoryRepo) CreateUser(_ context.Context, u entities.User) error {
	if _, ok := r.names[u.NormalizedUsername]; ok {
		return entities.ErrConflict
	}
	r.users[u.ID] = u
	r.names[u.NormalizedUsername] = u.ID
	return nil
}
func (r *memoryRepo) UserByID(_ context.Context, id string) (*entities.User, error) {
	v, ok := r.users[id]
	if !ok {
		return nil, entities.ErrNotFound
	}
	return &v, nil
}
func (r *memoryRepo) UserByNormalizedUsername(_ context.Context, n string) (*entities.User, error) {
	return r.UserByID(context.Background(), r.names[n])
}
func (r *memoryRepo) ListUsers(context.Context, entities.PageQuery) ([]entities.User, string, error) {
	return nil, "", nil
}
func (r *memoryRepo) UpdateUserStatus(_ context.Context, id, status string, at time.Time) error {
	v, ok := r.users[id]
	if !ok {
		return entities.ErrNotFound
	}
	v.Status, v.UpdatedAt = status, at
	r.users[id] = v
	return nil
}
func (r *memoryRepo) CreateOrganization(_ context.Context, o entities.Organization) error {
	if _, ok := r.organizationNames[o.NormalizedName]; ok {
		return entities.ErrConflict
	}
	r.organizations[o.ID] = o
	r.organizationNames[o.NormalizedName] = o.ID
	return nil
}
func (r *memoryRepo) OrganizationByID(_ context.Context, id string) (*entities.Organization, error) {
	v, ok := r.organizations[id]
	if !ok {
		return nil, entities.ErrNotFound
	}
	return &v, nil
}
func (r *memoryRepo) OrganizationByNormalizedName(_ context.Context, n string) (*entities.Organization, error) {
	return r.OrganizationByID(context.Background(), r.organizationNames[n])
}
func (r *memoryRepo) ListOrganizations(context.Context, entities.PageQuery) ([]entities.Organization, string, error) {
	return nil, "", nil
}
func (r *memoryRepo) UpdateOrganization(_ context.Context, o entities.Organization) error {
	r.organizations[o.ID] = o
	return nil
}
func membershipID(o, u string) string { return o + ":" + u }
func (r *memoryRepo) PutMembership(_ context.Context, m entities.Membership) error {
	r.memberships[membershipID(m.OrganizationID, m.UserID)] = m
	return nil
}
func (r *memoryRepo) Membership(_ context.Context, o, u string) (*entities.Membership, error) {
	v, ok := r.memberships[membershipID(o, u)]
	if !ok {
		return nil, entities.ErrNotFound
	}
	return &v, nil
}
func (r *memoryRepo) ListMemberships(_ context.Context, o string) ([]entities.Membership, error) {
	out := []entities.Membership{}
	for _, m := range r.memberships {
		if m.OrganizationID == o {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *memoryRepo) ListMembershipsForUser(_ context.Context, u string) ([]entities.Membership, error) {
	out := []entities.Membership{}
	for _, m := range r.memberships {
		if m.UserID == u {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *memoryRepo) CountActiveOrganizationAdmins(_ context.Context, o string) (int, error) {
	count := 0
	for _, m := range r.memberships {
		if m.OrganizationID == o && m.Role == entities.MembershipAdmin && r.users[m.UserID].Status == entities.StatusActive {
			count++
		}
	}
	return count, nil
}
func (r *memoryRepo) DeleteMembership(_ context.Context, o, u string) error {
	key := membershipID(o, u)
	if _, ok := r.memberships[key]; !ok {
		return entities.ErrNotFound
	}
	delete(r.memberships, key)
	return nil
}

type memoryAudit struct{ events []entities.AuditEvent }

func (a *memoryAudit) AppendAudit(_ context.Context, e entities.AuditEvent) error {
	a.events = append(a.events, e)
	return nil
}
func (a *memoryAudit) QueryAudit(context.Context, entities.AuditQuery) (*entities.AuditPage, error) {
	return nil, nil
}

func TestIdentityLifecycleAndLastAdmin(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	audit := &memoryAudit{}
	service := NewService(repo, audit)
	master := entities.Principal{Type: entities.PrincipalMaster}
	u1, err := service.CreateUser(ctx, master, " Admin@Example.com ")
	if err != nil || u1.Username != "admin@example.com" {
		t.Fatalf("create user: %v %#v", err, u1)
	}
	if _, err = service.CreateUser(ctx, master, "ADMIN@example.COM"); !errors.Is(err, entities.ErrConflict) {
		t.Fatalf("duplicate normalized username = %v", err)
	}
	u2, _ := service.CreateUser(ctx, master, "member@example.com")
	o1, _ := service.CreateOrganization(ctx, master, " Acme ")
	o2, _ := service.CreateOrganization(ctx, master, "Other")
	if _, err = service.AddMembership(ctx, master, o1.ID, u1.ID, entities.MembershipAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddMembership(ctx, master, o1.ID, u2.ID, entities.MembershipMember); err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddMembership(ctx, master, o2.ID, u1.ID, entities.MembershipAdmin); err != nil {
		t.Fatal(err)
	}
	if err = service.RemoveMembership(ctx, master, o1.ID, u1.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin removal=%v", err)
	}
	if err = service.ChangeMembershipRole(ctx, master, o1.ID, u2.ID, entities.MembershipAdmin); err != nil {
		t.Fatal(err)
	}
	if err = service.RemoveMembership(ctx, master, o1.ID, u1.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateUserKeyContext(ctx, u1.ID, o1.ID); !errors.Is(err, ErrMembershipNeeded) {
		t.Fatalf("removed member retained key context: %v", err)
	}
	if err = service.ValidateUserKeyContext(ctx, u1.ID, o2.ID); err != nil {
		t.Fatalf("valid second organization rejected: %v", err)
	}
	if len(audit.events) < 7 {
		t.Fatalf("audit events=%d", len(audit.events))
	}
}

func TestDisabledUserCannotBecomeMember(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	master := entities.Principal{Type: entities.PrincipalMaster}
	user, _ := service.CreateUser(context.Background(), master, "off@example.com")
	organization, _ := service.CreateOrganization(context.Background(), master, "Org")
	_ = service.SetUserStatus(context.Background(), master, user.ID, entities.StatusDisabled)
	if _, err := service.AddMembership(context.Background(), master, organization.ID, user.ID, entities.MembershipMember); !errors.Is(err, ErrInactiveUser) {
		t.Fatalf("disabled user add=%v", err)
	}
}
