package selfupdate

import (
	"strings"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func terminalUpdateMessage(result updateengine.Result) string {
	if result.Failure != nil && strings.TrimSpace(result.Failure.Message) != "" {
		return result.Failure.Message
	}
	return string(result.State)
}
