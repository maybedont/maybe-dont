package main

import (
	_ "embed"

	"github.com/maybedont/maybe-dont/cmd"
)

var (
	// version is set during build
	version = "development"
	// commit is set during build
	commit = "n/a"
	// date is set during build
	date = "n/a"

	//go:embed ai_rules.yaml
	aiRules []byte

	//go:embed cel_rules.yaml
	celRules []byte
)

func main() {
	cmd.Execute(version, commit, date, aiRules, celRules)
}
