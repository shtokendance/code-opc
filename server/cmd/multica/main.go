package main

import (
	"os"

	"github.com/multica-ai/multica/server/pkg/multicacli"
)

var (
	version     = "dev"
	commit      = "unknown"
	date        = "unknown"
	buildCommit = "unknown"
)

func main() {
	multicacli.SetVersionInfo(version, commit, date, buildCommit)
	os.Exit(multicacli.Execute(os.Args[1:]))
}
