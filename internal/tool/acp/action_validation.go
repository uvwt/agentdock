package acp

func isSessionAction(action string) bool {
	switch action {
	case "info", "new", "list", "inspect", "open", "update", "close", "delete":
		return true
	default:
		return false
	}
}
