package app

import "github.com/uvwt/agentdock/internal/textutil"

func truncateString(value string, maxBytes int) string {
	return textutil.SafeTruncateString(value, maxBytes).Text
}
