package entities

import "testing"

func TestNormalizeUsername(t *testing.T) {
	got, err := NormalizeUsername("  Person@Example.COM ")
	if err != nil || got != "person@example.com" {
		t.Fatalf("NormalizeUsername() = %q, %v", got, err)
	}
	for _, bad := range []string{"", "person", "Name <person@example.com>", "a@example.com\nBcc:x@y.z"} {
		if _, err := NormalizeUsername(bad); err == nil {
			t.Errorf("NormalizeUsername(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestAPIKeyValidateOwnerShape(t *testing.T) {
	tests := []struct {
		name string
		key  ApiKey
		ok   bool
	}{
		{"personal", ApiKey{OwnerType: OwnerUser, OwnerUserID: "usr_1"}, true},
		{"scoped personal", ApiKey{OwnerType: OwnerUser, OwnerUserID: "usr_1", ContextOrganizationID: "org_1"}, true},
		{"organization", ApiKey{OwnerType: OwnerOrganization, OwnerOrganizationID: "org_1", ContextOrganizationID: "org_1"}, true},
		{"missing user", ApiKey{OwnerType: OwnerUser}, false},
		{"two owners", ApiKey{OwnerType: OwnerUser, OwnerUserID: "usr_1", OwnerOrganizationID: "org_1"}, false},
		{"wrong context", ApiKey{OwnerType: OwnerOrganization, OwnerOrganizationID: "org_1", ContextOrganizationID: "org_2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.ValidateOwnerShape() == nil; got != tt.ok {
				t.Fatalf("valid = %v, want %v", got, tt.ok)
			}
		})
	}
}

func TestMembersManageIsValidScope(t *testing.T) {
	if !ValidScope(ScopeMembersManage) {
		t.Fatal("members:manage must be a valid scope")
	}
}
