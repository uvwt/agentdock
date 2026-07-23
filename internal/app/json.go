package app

import toolcore "github.com/uvwt/agentdock/internal/tool/core"

func remarshal(input, output any) error {
	return toolcore.Remarshal(input, output)
}
