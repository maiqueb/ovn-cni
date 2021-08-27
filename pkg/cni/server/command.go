package server

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	serverSocketName      = "ovn-cni.sock"
)

const (
	Rundir        = "Rundir"
	DefaultRundir = "/var/run/ovn-cni/cni/"
)

func SocketPath(rundir string) string {
	return filepath.Join(rundir, serverSocketName)
}

func FilesystemPreRequirements(rundir string) error {
	socketpath := SocketPath(rundir)
	if err := os.RemoveAll(rundir); err != nil && !os.IsNotExist(err) {
		info, err := os.Stat(rundir)
		if err != nil {
			return fmt.Errorf("failed to stat old pod info socket directory %s: %v", rundir, err)
		}
		// Owner must be root
		tmp := info.Sys()
		statt, ok := tmp.(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to read pod info socket directory stat info: %T", tmp)
		}
		if statt.Uid != 0 {
			return fmt.Errorf("insecure owner of pod info socket directory %s: %v", rundir, statt.Uid)
		}

		// Check permissions
		if info.Mode()&0777 != 0700 {
			return fmt.Errorf("insecure permissions on pod info socket directory %s: %v", rundir, info.Mode())
		}

		// Finally remove the socket file so we can re-create it
		if err := os.Remove(socketpath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove old pod info socket %s: %v", socketpath, err)
		}
	}
	if err := os.MkdirAll(rundir, 0700); err != nil {
		return fmt.Errorf("failed to create pod info socket directory %s: %v", rundir, err)
	}
	return nil
}
