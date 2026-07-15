//go:build !darwin && !linux && !windows

package selfupdate

import "context"

func detectManagedService(context.Context, string) managedService {
	return nil
}

func platformHealthCandidates(context.Context, string) []string {
	return nil
}

func signLocalReplacement(context.Context, string) error {
	return nil
}
