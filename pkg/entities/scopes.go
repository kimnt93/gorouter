package entities

const (
	ScopeChat              = "chat"
	ScopeUsageRead         = "usage:read"
	ScopeKeysManage        = "keys:manage"
	ScopeCredentialsManage = "credentials:manage"
	ScopeModelsManage      = "models:manage"
	ScopeCachePurge        = "cache:purge"
	ScopeMembersManage     = "members:manage"
)

var AllScopes = []string{
	ScopeChat,
	ScopeUsageRead,
	ScopeKeysManage,
	ScopeCredentialsManage,
	ScopeModelsManage,
	ScopeCachePurge,
	ScopeMembersManage,
}

func ValidScope(s string) bool {
	for _, v := range AllScopes {
		if v == s {
			return true
		}
	}
	return false
}

type Session struct {
	Role           string
	KeyID          string
	TenantID       string
	Scopes         []string
	Expires        int64
	PrincipalType  string   `json:"principal_type,omitempty"`
	UserID         string   `json:"user_id,omitempty"`
	Username       string   `json:"username,omitempty"`
	OrganizationID string   `json:"organization_id,omitempty"`
	MembershipRole string   `json:"membership_role,omitempty"`
	AllowedModels  []string `json:"-"`
}

const (
	RoleMaster = "master"
	RoleAPIKey = "apikey"
)

func (s *Session) Has(scope string) bool {
	if s == nil {
		return false
	}
	if s.Role == RoleMaster {
		return true
	}
	for _, sc := range s.Scopes {
		if sc == scope {
			return true
		}
	}
	return false
}

func (s *Session) IsMaster() bool { return s != nil && s.Role == RoleMaster }
