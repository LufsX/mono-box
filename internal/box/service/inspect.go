//go:build linux || android

package service

// InspectPID verifies the managed core identity without waiting for a lifecycle
// transition or changing PID records and module status.
func (r *Runtime) InspectPID() (int, error) {
	_, loc, lock, err := r.serviceLockedRuntime(0)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	id, err := serviceRunning(loc)
	return id.PID, err
}
