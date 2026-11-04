package auth

import "sort"

// Permissions are the scopes from the security specification, plus workspace:manage
// (rename/delete the workspace) which only owners hold.
const (
	LogsIngest      = "logs:ingest"
	LogsRead        = "logs:read"
	LogsExport      = "logs:export"
	LogsDelete      = "logs:delete"
	ProjectsWrite   = "projects:write"
	TokensManage    = "tokens:manage"
	MembersManage   = "members:manage"
	StorageManage   = "storage:manage"
	AlertsManage    = "alerts:manage"
	WorkspaceManage = "workspace:manage"
)

var allPermissions = []string{LogsIngest, LogsRead, LogsExport, LogsDelete, ProjectsWrite, TokensManage,
	MembersManage, StorageManage, AlertsManage, WorkspaceManage}

// SystemRoles are created for every new workspace. The instance admin is not a workspace role:
// it grants no access to tenant data.
var SystemRoles = map[string][]string{
	"owner":     allPermissions,
	"admin":     {LogsIngest, LogsRead, LogsExport, LogsDelete, ProjectsWrite, TokensManage, MembersManage, StorageManage, AlertsManage},
	"developer": {LogsIngest, LogsRead, LogsExport, ProjectsWrite, TokensManage, AlertsManage},
	"analyst":   {LogsRead, LogsExport},
	"viewer":    {LogsRead},
}

// SystemRoleNames in display order.
func SystemRoleNames() []string {
	names := []string{"owner", "admin", "developer", "analyst", "viewer"}
	return names
}

// TokenScopes are the scopes an API token may carry. A token can never exceed its creator's permissions.
var TokenScopes = map[string]bool{LogsIngest: true, LogsRead: true, LogsExport: true}

// Admin scopes are held by instance admin tokens and the local admin socket (ADR 0019). None of them
// reaches tenant logs.
const (
	AdminRead     = "admin:read"
	AdminUsers    = "admin:users"
	AdminSettings = "admin:settings"
	AdminTenancy  = "admin:tenancy"
	AdminStorage  = "admin:storage"
)

var AdminScopes = map[string]bool{AdminRead: true, AdminUsers: true, AdminSettings: true, AdminTenancy: true, AdminStorage: true}

// OperatorPermissions are what an operator (socket or admin token) holds inside every workspace: tenancy
// management, plus logs:ingest so it can issue ingest tokens. Never logs:read, logs:export or logs:delete.
var OperatorPermissions = []string{LogsIngest, ProjectsWrite, TokensManage, MembersManage, WorkspaceManage}

// PermSet is a set of permissions.
type PermSet map[string]bool

func NewPermSet(p []string) PermSet {
	s := PermSet{}
	for _, x := range p {
		s[x] = true
	}
	return s
}

func (s PermSet) Has(p string) bool { return s[p] }

func (s PermSet) List() []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
