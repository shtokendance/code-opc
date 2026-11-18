package embeddeddaemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

// DefaultManager is the package-level embedded daemon manager instance.
var DefaultManager = NewManager(slog.Default())

type activeInstance struct {
	profile    string
	cancel     context.CancelFunc
	done       chan struct{}
	daemon     *daemon.Daemon
	lockFile   *os.File
	startedAt  time.Time
	err        error
	runErrOnce sync.Once
}

// Manager manages in-process and host daemon instances for desktop clients.
type Manager struct {
	mu        sync.Mutex
	instances map[string]*activeInstance
	logger    *slog.Logger
}

// NewManager creates a new Manager with the provided logger.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		instances: make(map[string]*activeInstance),
		logger:    logger,
	}
}

// ProfileDir returns the state directory for a given profile.
func ProfileDir(profile string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	if profile == "" {
		return filepath.Join(home, ".multica"), nil
	}
	return filepath.Join(home, ".multica", "profiles", profile), nil
}

// HealthPortForProfile calculates a deterministic fallback health port from the profile name.
func HealthPortForProfile(profile string) int {
	if profile == "" {
		return daemon.DefaultHealthPort
	}
	var h int
	for _, b := range []byte(profile) {
		h += int(b)
	}
	return daemon.DefaultHealthPort + 1 + (h % 1000)
}

// ProbeLocalRuntimes performs in-process detection of installed AI agent CLIs.
// This executes pure Go inspection without shelling out to an external multica CLI binary.
func (m *Manager) ProbeLocalRuntimes(ctx context.Context, profile string) (*ProbeResult, error) {
	cfg, err := daemon.LoadConfig(daemon.Overrides{
		Profile:       profile,
		AllowNoAgents: true,
	})
	if err != nil {
		return nil, fmt.Errorf("probe local runtimes config: %w", err)
	}

	res := &ProbeResult{
		ProbeResult:     "success",
		RuntimeCount:    len(cfg.Agents),
		OnlineCount:     len(cfg.Agents),
		OfflineCount:    0,
		ProviderSummary: make(map[string]int, len(cfg.Agents)),
		Agents:          make(map[string]AgentInfo, len(cfg.Agents)),
	}

	for provider, entry := range cfg.Agents {
		res.ProviderSummary[provider]++
		res.Agents[provider] = AgentInfo{
			Provider: provider,
			Path:     entry.Path,
			Version:  "",
		}
	}

	return res, nil
}

// SyncToken writes the authentication token and server URL into the profile's config.json.
func (m *Manager) SyncToken(profile, token, serverURL string) error {
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	if token != "" {
		cfg.Token = token
	}
	if serverURL != "" {
		cfg.ServerURL = serverURL
	}
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return fmt.Errorf("save config for profile %q: %w", profile, err)
	}
	return nil
}

// LoadToken reads the stored token for a given profile.
func (m *Manager) LoadToken(profile string) (string, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return "", err
	}
	return cfg.Token, nil
}

// Start launches the daemon in-process inside a background goroutine.
func (m *Manager) Start(ctx context.Context, opts DaemonOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	profile := opts.Profile

	// Check if already running in this process
	if inst, exists := m.instances[profile]; exists {
		select {
		case <-inst.done:
			delete(m.instances, profile)
		default:
			return fmt.Errorf("daemon profile %q is already running in this process", profile)
		}
	}

	// Resolve profile directory
	pDir, err := ProfileDir(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(pDir, 0o755); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}

	if opts.AuthToken != "" || opts.ServerURL != "" {
		_ = m.SyncToken(profile, opts.AuthToken, opts.ServerURL)
	}

	// Acquire single-instance profile lock
	lockPath := filepath.Join(pDir, "daemon.lock")
	lockFile, acquired, err := tryLockProfile(lockPath)
	if err != nil {
		return fmt.Errorf("lock profile %q: %w", profile, err)
	}
	if !acquired {
		return fmt.Errorf("daemon profile %q is locked by another process", profile)
	}

	// Prepare Overrides (embedded in-process mode does not bind a local HTTP health port).
	overrides := daemon.Overrides{
		Profile:           profile,
		ServerURL:         opts.ServerURL,
		WorkspacesRoot:    opts.WorkspacesRoot,
		DeviceName:        opts.DeviceName,
		RuntimeName:       opts.RuntimeName,
		PollInterval:      opts.PollInterval,
		DisableAutoReload: true, // Desktop controls lifecycle directly
		DisableAutoUpdate: true, // Desktop is packaged as native app
	}

	cfg, err := daemon.LoadConfig(overrides)
	if err != nil {
		_ = lockFile.Close()
		return fmt.Errorf("load daemon config: %w", err)
	}

	if opts.LaunchedBy != "" {
		cfg.LaunchedBy = opts.LaunchedBy
	} else {
		cfg.LaunchedBy = "desktop"
	}
	if opts.CLIVersion != "" {
		cfg.CLIVersion = opts.CLIVersion
	} else {
		cfg.CLIVersion = "0.9.0"
	}
	if opts.CLICommit != "" {
		cfg.CLICommit = opts.CLICommit
	} else {
		cfg.CLICommit = "wails-desktop"
	}

	d := daemon.New(cfg, m.logger)
	// Embedded daemon runs completely in-process; avoid spawning external subprocess helpers
	d.DisableExecutionIsolation()
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	inst := &activeInstance{
		profile:   profile,
		cancel:    daemonCancel,
		done:      doneCh,
		daemon:    d,
		lockFile:  lockFile,
		startedAt: time.Now(),
	}

	m.instances[profile] = inst

	go func() {
		defer close(doneCh)
		defer func() {
			_ = lockFile.Close()
			m.mu.Lock()
			if curr, ok := m.instances[profile]; ok && curr == inst {
				delete(m.instances, profile)
			}
			m.mu.Unlock()
		}()

		runErr := d.RunEmbedded(daemonCtx)
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			inst.runErrOnce.Do(func() {
				inst.err = runErr
			})
			m.logger.Error("embedded daemon stopped with error", "profile", profile, "err", runErr)
		}
	}()

	return nil
}

// StopAll stops all currently running embedded daemon instances.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	profiles := make([]string, 0, len(m.instances))
	for p := range m.instances {
		profiles = append(profiles, p)
	}
	m.mu.Unlock()

	for _, p := range profiles {
		_ = m.Stop(ctx, p)
	}
}

// Stop gracefully terminates an in-process daemon instance.
func (m *Manager) Stop(ctx context.Context, profile string) error {
	m.mu.Lock()
	inst, exists := m.instances[profile]
	m.mu.Unlock()

	if !exists {
		return nil
	}

	inst.cancel()
	select {
	case <-inst.done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timed out waiting for embedded daemon to stop")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Restart stops and restarts the embedded daemon.
func (m *Manager) Restart(ctx context.Context, opts DaemonOptions) error {
	_ = m.Stop(ctx, opts.Profile)
	time.Sleep(100 * time.Millisecond)
	return m.Start(ctx, opts)
}

// GetStatus inspects the in-memory operational status of the embedded daemon.
func (m *Manager) GetStatus(_ context.Context, profile string) (*DaemonStatus, error) {
	m.mu.Lock()
	inst, exists := m.instances[profile]
	m.mu.Unlock()

	status := &DaemonStatus{
		Running: false,
		Profile: profile,
	}

	if !exists {
		return status, nil
	}

	select {
	case <-inst.done:
		if inst.err != nil {
			status.Error = inst.err.Error()
		}
		return status, nil
	default:
	}

	snap := inst.daemon.HealthSnapshot(inst.startedAt)
	rawBytes, _ := json.Marshal(snap)
	var healthData map[string]any
	_ = json.Unmarshal(rawBytes, &healthData)

	wsIDs := make([]string, 0, len(snap.Workspaces))
	for _, ws := range snap.Workspaces {
		wsIDs = append(wsIDs, ws.ID)
	}

	status.Running = true
	status.PID = snap.PID
	status.ServerURL = snap.ServerURL
	status.Providers = snap.Agents
	status.Workspaces = wsIDs
	status.RawHealth = healthData
	return status, nil
}
