package orgmodel

import (
	"context"
	"errors"
	"github.com/kimnt93/gorouter/pkg/entities"
	"testing"
)

type memoryRepo struct {
	offers []entities.OrganizationModel
	grants []entities.OrganizationModelGrant
}

func (r *memoryRepo) List(context.Context, string) ([]entities.OrganizationModel, error) {
	return r.offers, nil
}
func (r *memoryRepo) Put(_ context.Context, v entities.OrganizationModel) error {
	r.offers = append(r.offers, v)
	return nil
}
func (r *memoryRepo) Grants(_ context.Context, _ string, user string) ([]entities.OrganizationModelGrant, error) {
	out := []entities.OrganizationModelGrant{}
	for _, g := range r.grants {
		if g.UserID == user {
			out = append(out, g)
		}
	}
	return out, nil
}
func (r *memoryRepo) PutGrant(_ context.Context, v entities.OrganizationModelGrant) error {
	r.grants = append(r.grants, v)
	return nil
}
func (r *memoryRepo) Reserve(context.Context, entities.ModelBudgetReservation) error { return nil }
func (r *memoryRepo) Settle(context.Context, string, string, float64) error          { return nil }

type identities struct{}

func (identities) OrganizationByID(_ context.Context, id string) (*entities.Organization, error) {
	if id != "org" {
		return nil, entities.ErrNotFound
	}
	return &entities.Organization{ID: "org", Name: "xno", Status: entities.StatusActive}, nil
}
func (identities) ListMembershipsForUser(_ context.Context, id string) ([]entities.Membership, error) {
	if id == "foreign" {
		return nil, nil
	}
	return []entities.Membership{{OrganizationID: "org", UserID: id}}, nil
}
func (identities) Membership(_ context.Context, org, id string) (*entities.Membership, error) {
	if org != "org" || id == "foreign" {
		return nil, entities.ErrNotFound
	}
	role := entities.MembershipMember
	if id == "admin" {
		role = entities.MembershipAdmin
	}
	return &entities.Membership{OrganizationID: org, UserID: id, Role: role}, nil
}
func (identities) UserByID(_ context.Context, id string) (*entities.User, error) {
	return &entities.User{ID: id, Status: entities.StatusActive}, nil
}

type models struct{}

func (models) List(context.Context) ([]entities.ModelDef, error) {
	return []entities.ModelDef{{Name: "cx/base", Enabled: true, UpstreamModel: "base", Routes: []entities.ModelRoute{{CredentialID: "source", UpstreamModel: "base", Enabled: true, Weight: 1}}}}, nil
}

type credentials struct{}

func (credentials) List(context.Context) ([]entities.Credential, error) {
	return []entities.Credential{{ID: "source", OwnerUserID: "admin", Status: entities.StatusActive}}, nil
}
func TestAliasGroupAssignmentsAndAuthority(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{}
	svc := &Service{Repo: repo, Identity: identities{}, Models: models{}, Credentials: credentials{}}
	admin := entities.Principal{Type: entities.PrincipalUser, UserID: "admin", Scopes: []string{entities.ScopeModelsManage}}
	limit := 2.0
	alias, err := svc.Publish(ctx, admin, "org", "xno-lite", "alias", []string{"cx/base"}, &limit, true)
	if err != nil || alias.Name != "xno/xno-lite" {
		t.Fatalf("alias=%+v %v", alias, err)
	}
	group, err := svc.Publish(ctx, admin, "org", "default", "group", []string{alias.Name}, &limit, true)
	if err != nil || group.Name != "xno/g/default" {
		t.Fatalf("group=%+v %v", group, err)
	}
	if _, err = svc.Assign(ctx, admin, "org", group.Name, "user", &limit, true); err != nil {
		t.Fatal(err)
	}
	available, err := svc.Available(ctx, "user")
	if err != nil || len(available) != 1 || len(available[0].Routes) != 1 || len(available[0].Routes[0].Charges) != 3 {
		t.Fatalf("resolution=%+v err=%v", available, err)
	}
	if available, err = svc.Available(ctx, "foreign"); err != nil || len(available) != 0 {
		t.Fatal("foreign access")
	}
	member := admin
	member.UserID = "user"
	if _, err = svc.Publish(ctx, member, "org", "bad", "alias", []string{"cx/base"}, nil, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("member published")
	}
	if _, err = svc.Publish(ctx, admin, "org", "cycle", "group", []string{group.Name}, nil, true); !errors.Is(err, ErrInvalid) {
		t.Fatal("nested group accepted")
	}
	if _, err = svc.Publish(ctx, admin, "org", "bad", "alias", []string{"private/foreign"}, nil, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("foreign source accepted")
	}
}
