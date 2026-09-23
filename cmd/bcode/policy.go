package main

import (
	"github.com/akynte/boundedcode/internal/supervisor"

	"github.com/akynte/boundedcode/internal/broker"
	"github.com/akynte/boundedcode/internal/config"
)

// policyFrom translates the operator's gate configuration into a broker policy.
func policyFrom(g config.GateConfig) broker.Policy {
	return supervisor.GatePolicy(g)
}
