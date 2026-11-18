package embeddeddaemon

import (
	"time"
)

// AgentInfo describes an individual discovered AI provider/runtime.
type AgentInfo struct {
	Provider string `json:"provider"`
	Path     string `json:"path"`
	Version  string `json:"version"`
}

// ProbeResult contains the structured response of local runtime probing.
type ProbeResult struct {
	ProbeResult     string               `json:"probeResult"`
	RuntimeCount    int                  `json:"runtimeCount"`
	ProviderSummary map[string]int       `json:"providerSummary"`
	OnlineCount     int                  `json:"onlineCount"`
	OfflineCount    int                  `json:"offlineCount"`
	Agents          map[string]AgentInfo `json:"agents,omitempty"`
}

// DaemonOptions configures the in-process daemon.
type DaemonOptions struct {
	Profile        string        `json:"profile"`
	ServerURL      string        `json:"server_url"`
	AuthToken      string        `json:"auth_token"`
	WorkspacesRoot string        `json:"workspaces_root"`
	DeviceName     string        `json:"device_name"`
	RuntimeName    string        `json:"runtime_name"`
	HealthPort     int           `json:"health_port"`
	PollInterval   time.Duration `json:"poll_interval"`
	LaunchedBy     string        `json:"launched_by"`
	CLIVersion     string        `json:"cli_version,omitempty"`
	CLICommit      string        `json:"cli_commit,omitempty"`
}

// DaemonStatus represents the live operational status of the embedded daemon.
type DaemonStatus struct {
	Running         bool           `json:"running"`
	Profile         string         `json:"profile"`
	PID             int            `json:"pid"`
	Port            int            `json:"port"`
	ServerURL       string         `json:"server_url,omitempty"`
	Providers       []string       `json:"providers,omitempty"`
	Workspaces      []string       `json:"workspaces,omitempty"`
	TasksClaimed    int            `json:"tasks_claimed"`
	LastHeartbeatAt string         `json:"last_heartbeat_at,omitempty"`
	RawHealth       map[string]any `json:"raw_health,omitempty"`
	Error           string         `json:"error,omitempty"`
}
