package main

import (
	_ "embed"

	"github.com/maybedont/maybe-dont/cmd"
	"github.com/maybedont/maybe-dont/internal/metrics"
)

var (
	// version is set during build
	version = "development"
	// commit is set during build
	commit = "n/a"
	// date is set during build
	date = "n/a"
	// metricsDataset is set during build (default Axiom dataset for metrics)
	metricsDataset = ""
	// metricsAPIToken is set during build (default Axiom API token for metrics)
	metricsAPIToken = ""

	//go:embed ai_rules.yaml
	aiRules []byte

	//go:embed cel_rules.yaml
	celRules []byte

	//go:embed ai_response_rules.yaml
	aiResponseRules []byte

	//go:embed cel_response_rules.yaml
	celResponseRules []byte
)

func main() {
	// Create metrics configuration from build-time variables
	metricsCfg := metrics.Config{
		Dataset:  metricsDataset,
		APIToken: metricsAPIToken,
	}

	cmd.Execute(version, commit, date, metricsCfg, aiRules, celRules, aiResponseRules, celResponseRules)
}
