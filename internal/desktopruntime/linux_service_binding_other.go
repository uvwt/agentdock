//go:build !linux

package desktopruntime

func DetectLinuxServiceBinding(string) (LinuxServiceBinding, error) {
	return LinuxServiceBinding{}, nil
}

func (b LinuxServiceBinding) SyncAgentDockHomeOwnership(...string) error {
	return nil
}
