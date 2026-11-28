package multicacli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

var (
	version     = "dev"
	commit      = "unknown"
	date        = "unknown"
	buildCommit = "unknown"

	Version     = "dev"
	Commit      = "unknown"
	Date        = "unknown"
	BuildCommit = "unknown"
)

// SetVersionInfo configures build version metadata.
func SetVersionInfo(ver, com, d, bCommit string) {
	if ver != "" {
		version = ver
		Version = ver
	}
	if com != "" {
		commit = com
		Commit = com
	}
	if d != "" {
		date = d
		Date = d
	}
	if bCommit != "" {
		buildCommit = bCommit
		BuildCommit = bCommit
	}
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)\ngo: %s, os/arch: %s/%s", Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	cli.ClientVersion = Version
}

var debugFlag bool

var rootCmd = &cobra.Command{
	Use:           "multica",
	Short:         "Multica CLI — local agent runtime and management tool",
	Long:          "Work seamlessly with Multica from the command line.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	SetVersionInfo(Version, Commit, Date, BuildCommit)
	rootCmd.SetVersionTemplate("multica {{.Version}}\n")

	rootCmd.PersistentFlags().String("server-url", "", "Multica server URL (env: MULTICA_SERVER_URL)")
	rootCmd.PersistentFlags().String("workspace-id", "", "Workspace ID (env: MULTICA_WORKSPACE_ID)")
	rootCmd.PersistentFlags().String("profile", "", "Configuration profile name (e.g. dev) — isolates config, daemon state, and workspaces")
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print full error details on failure (env: MULTICA_DEBUG)")

	// Core commands
	issueCmd.GroupID = groupCore
	inboxCmd.GroupID = groupCore
	issueStatusCatalogCmd.GroupID = groupCore
	projectCmd.GroupID = groupCore
	workflowCmd.GroupID = groupCore
	labelCmd.GroupID = groupCore
	propertyCmd.GroupID = groupCore
	quickActionCmd.GroupID = groupCore
	viewCmd.GroupID = groupCore
	agentCmd.GroupID = groupCore
	autopilotCmd.GroupID = groupCore
	workspaceCmd.GroupID = groupCore
	repoCmd.GroupID = groupCore
	skillCmd.GroupID = groupCore
	squadCmd.GroupID = groupCore
	chatCmd.GroupID = groupCore

	// Runtime commands
	daemonCmd.GroupID = groupRuntime
	runtimeCmd.GroupID = groupRuntime

	// Additional commands
	authCmd.GroupID = groupAdditional
	userCmd.GroupID = groupAdditional
	loginCmd.GroupID = groupAdditional
	setupCmd.GroupID = groupAdditional
	attachmentCmd.GroupID = groupAdditional
	configCmd.GroupID = groupAdditional
	updateCmd.GroupID = groupAdditional
	versionCmd.GroupID = groupAdditional

	rootCmd.AddCommand(issueCmd)
	rootCmd.AddCommand(inboxCmd)
	rootCmd.AddCommand(docCmd)
	rootCmd.AddCommand(issueStatusCatalogCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(workflowCmd)
	rootCmd.AddCommand(labelCmd)
	rootCmd.AddCommand(propertyCmd)
	rootCmd.AddCommand(quickActionCmd)
	rootCmd.AddCommand(viewCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(autopilotCmd)
	rootCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(environmentCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(squadCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(runtimeCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(attachmentCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd)

	initHelp(rootCmd)
}

// IsCLIInvocation determines if the provided args represent an invocation of the multica CLI
// rather than starting a GUI application.
func IsCLIInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Direct execution environment preparation or Cursor relay helpers
	if len(args) == 2 && (args[1] == execenv.PreparationHelperArg || args[1] == agent.CursorHookHelperArg) {
		return true
	}
	// If invoked without subcommands
	if len(args) == 1 {
		base := filepath.Base(args[0])
		return base == "multica" || base == "multica.exe"
	}
	first := args[1]
	// macOS Finder passes -psn_... when launching app bundles
	if strings.HasPrefix(first, "-psn_") {
		return false
	}
	// Custom URL schemes (e.g. codeopc://, onepc://, multica://) launch the GUI
	if strings.HasPrefix(first, "codeopc://") || strings.HasPrefix(first, "onepc://") || strings.HasPrefix(first, "multica://") {
		return false
	}
	return true
}

// Execute runs the multica CLI with the given arguments and returns the exit code.
func Execute(args []string) int {
	if len(args) == 1 && args[0] == execenv.PreparationHelperArg {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		if err := execenv.RunPreparationHelper(os.Stdin, os.Stdout, logger); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == agent.CursorHookHelperArg {
		agent.RunCursorHookRelay(os.Stdin, os.Stdout, os.Getenv)
		return 0
	}
	cli.CleanupStaleUpdateArtifacts()
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		if err != errSilent {
			fmt.Fprintln(os.Stderr, cli.FormatError(err, debugFlag))
		}
		return cli.ExitCodeFor(err)
	}
	return 0
}
