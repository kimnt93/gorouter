package entities

const (
	ScopeChat              = "chat"
	ScopeUsageRead         = "usage:read"
	ScopeKeysManage        = "keys:manage"
	ScopeCredentialsManage = "credentials:manage"
	ScopeModelsManage      = "models:manage"
	ScopeCachePurge        = "cache:purge"
)

var AllScopes = []string{
	ScopeChat,
	ScopeUsageRead,
	ScopeKeysManage,
	ScopeCredentialsManage,
	ScopeModelsManage,
	ScopeCachePurge,
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
	Role     string
	KeyID    string
	TenantID string
	Scopes   []string
	Expires  int64
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
