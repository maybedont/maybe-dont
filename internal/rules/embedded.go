package rules

import (
	_ "embed"
)

//go:embed cel_rules.yaml
var DefaultCELRules []byte

//go:embed ai_rules.yaml
var DefaultAIRules []byte
