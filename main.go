package main

import (
	_ "embed"

	"github.com/maybedont/maybe-dont/cmd"
)

var (
	// version is set during build
	version = "development"
	// commit is set during build
	versionCommit = "n/a"

	//go:embed ai_rules.yaml
	aiRules []byte

	//go:embed cel_rules.yaml
	celRules []byte
)

func main() {
	cmd.Execute(version, versionCommit, aiRules, celRules)
}
