package embeddeddaemon

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// HandleInternalCLIHelper checks if the process was invoked as an execenv preparation
// helper or a Cursor hook relay, executing it and exiting if so.
// Returns true if a helper was handled, false otherwise.
func HandleInternalCLIHelper() bool {
	if len(os.Args) == 2 && os.Args[1] == execenv.PreparationHelperArg {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		if err := execenv.RunPreparationHelper(os.Stdin, os.Stdout, logger); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	if len(os.Args) == 2 && os.Args[1] == agent.CursorHookHelperArg {
		agent.RunCursorHookRelay(os.Stdin, os.Stdout, os.Getenv)
		return true
	}
	return false
}
