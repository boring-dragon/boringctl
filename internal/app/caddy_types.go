package app

const (
	CaddyVisibilityInternal = "internal"
	CaddyVisibilityPublic   = "public"
	CaddyAppGeneric         = "generic"
	CaddyAppPHP             = "php"
	CaddyAppRealtime        = "realtime"
	CaddyAppLargeUpload     = "large-upload"
	CaddyAppStatic          = "static"
	CaddyAppSPA             = "spa"
	CaddyAppPHPFPM          = "php-fpm"
)

type CaddySiteRequest struct {
	Domain              string
	Visibility          string
	AppType             string
	UpstreamScheme      string
	UpstreamHost        string
	UpstreamPort        int
	RootPath            string
	UseWAF              bool
	WAFExplicit         bool
	InsecureTLSUpstream bool
	Force               bool
	Deploy              bool
	DryRun              bool
	CommonProxySnippet  string
	InternalACLSnippet  string
}

type CaddySiteResult struct {
	Domain                string
	Slug                  string
	SitePath              string
	RulePath              string
	Upstream              string
	RootPath              string
	Visibility            string
	AppType               string
	Deployed              bool
	DeploySummary         string
	DeployBackup          string
	DeployRolledBack      bool
	DeployRollbackSummary string
}

type CaddySiteSummary struct {
	Domain     string
	Slug       string
	Visibility string
	AppType    string
	Upstream   string
	RootPath   string
	UseWAF     bool
	Managed    bool
	SitePath   string
	RulePath   string
}

type CaddyRemoveResult struct {
	Domain                string
	SitePath              string
	RulePath              string
	Removed               bool
	Deployed              bool
	DeploySummary         string
	DeployBackup          string
	DeployRolledBack      bool
	DeployRollbackSummary string
}

type CaddyDeployResult struct {
	Validated       bool
	Applied         bool
	Backup          string
	RolledBack      bool
	RollbackSummary string
	Summary         string
	Smoke           []CaddySmokeResult
}

type CaddyDeployOptions struct {
	Apply bool
	Smoke bool
}

type CaddySmokeResult struct {
	Domain     string
	URL        string
	StatusCode int
	Expected   string
	Passed     bool
	Error      string
}

type CaddyRollbackResult struct {
	Backup  string
	Applied bool
	Summary string
}

type CaddyTemplate struct {
	Name        string
	Label       string
	Description string
	NeedsTarget bool
	NeedsRoot   bool
	DefaultRoot string
	DefaultWAF  bool
}
