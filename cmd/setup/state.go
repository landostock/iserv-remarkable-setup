package main

import (
	"sync"
	"time"
)

type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Source struct {
	Name          string `json:"name"`
	IServPath     string `json:"iserv_path"`
	RemarkableDir string `json:"remarkable_dir"`
}

type InstallConfig struct {
	Region          string              `json:"region"`
	FlyOrgSlug      string              `json:"fly_org_slug,omitempty"`
	AdminPassword   string              `json:"admin_password"`
	IntervalMinutes int                 `json:"interval_minutes"`
	Schedule        map[string][]Window `json:"schedule"`
	LookbackDays    int                 `json:"lookback_days"`
	MaxFileMB       int                 `json:"max_file_mb"`
	Extensions      []string            `json:"extensions"`
	Sources         []Source            `json:"sources"`
}

type secrets struct {
	IServURL      string
	IServUser     string
	IServPassword string
	DeviceToken   string
	UserToken     string
}

type installMeta struct {
	AppName       string    `json:"app_name"`
	WorkerName    string    `json:"worker_name"`
	DashboardURL  string    `json:"dashboard_url"`
	Region        string    `json:"region"`
	MachineID     string    `json:"machine_id,omitempty"`
	KVNamespaceID string    `json:"kv_namespace_id,omitempty"`
	FlyTokenUntil string    `json:"fly_token_until,omitempty"`
	Version       string    `json:"version,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type verificationStats struct {
	Checked     int `json:"checked"`
	Uploaded    int `json:"uploaded"`
	Unchanged   int `json:"unchanged"`
	Failed      int `json:"failed"`
	Deferred    int `json:"deferred"`
	WouldUpload int `json:"would_upload"`
}

type verificationResult struct {
	FinishedAt string            `json:"finished_at,omitempty"`
	Outcome    string            `json:"outcome,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Message    string            `json:"message,omitempty"`
	Stats      verificationStats `json:"stats"`
}

type state struct {
	mu sync.RWMutex

	Language            string              `json:"-"`
	CostAccepted        bool                `json:"cost_accepted"`
	FlyLoggedIn         bool                `json:"fly_logged_in"`
	FlyOrganizations    []flyOrganization   `json:"fly_organizations,omitempty"`
	CloudflareLoggedIn  bool                `json:"cloudflare_logged_in"`
	IServConnected      bool                `json:"iserv_connected"`
	RemarkableConnected bool                `json:"remarkable_connected"`
	InstallStatus       string              `json:"install_status"`
	InstallPhase        string              `json:"install_phase,omitempty"`
	InstallProgress     int                 `json:"install_progress,omitempty"`
	InstallError        string              `json:"install_error,omitempty"`
	MaintenanceStatus   string              `json:"maintenance_status,omitempty"`
	MaintenanceAction   string              `json:"maintenance_action,omitempty"`
	MaintenanceError    string              `json:"maintenance_error,omitempty"`
	Verification        *verificationResult `json:"verification,omitempty"`
	DiagnosticsPath     string              `json:"diagnostics_path,omitempty"`
	Logs                []string            `json:"logs"`
	Meta                *installMeta        `json:"meta,omitempty"`
	Secrets             secrets             `json:"-"`
	Config              InstallConfig       `json:"config"`
	RemarkableFolders   []string            `json:"-"`
}

func defaultConfig() InstallConfig {
	school := []Window{{Start: "07:30", End: "12:50"}, {Start: "13:30", End: "15:10"}}
	schedule := map[string][]Window{}
	for day := 1; day <= 7; day++ {
		key := string(rune('0' + day))
		if day <= 5 {
			schedule[key] = append([]Window(nil), school...)
		} else {
			schedule[key] = []Window{}
		}
	}
	return InstallConfig{
		Region: "fra", IntervalMinutes: 10, Schedule: schedule,
		LookbackDays: 30, MaxFileMB: 95, Extensions: []string{"pdf", "epub"}, Sources: []Source{},
	}
}
