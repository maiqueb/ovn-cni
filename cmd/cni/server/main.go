package main

import (
	"flag"
	"fmt"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"net"
	"os"
	"path/filepath"

	cniserver "github.com/maiqueb/ovn-cni/pkg/cni/server"

	"k8s.io/klog"
)
const (
	defaultConfigFilename = "ovncnihandler"
	envPrefix             = "ovncni_"
	serverSocketName      = "ovn-cni.sock"
)

var (
	rundir string
)

func main() {
	flag.StringVar(&rundir, cniserver.Rundir, cniserver.DefaultRundir, "The directory where the unix socket for the thick plugin will live")

	klog.InitFlags(nil)
	flag.Parse()

	klog.Infof("Starting CNI server. Socket file at: %s", cniserver.SocketPath(rundir))
	if err := cniserver.FilesystemPreRequirements(rundir); err != nil {
		klog.Errorf("failed creating pre-requirements: %v", err)
		os.Exit(1)
	}

	server, err := cniserver.NewCNIServer(rundir)
	if err != nil {
		klog.Exit("failed to create the server: %v", err)
	}

	socketpath := socketPath(rundir)
	l, err := net.Listen("unix", socketpath)
	if err != nil {
		klog.Exit("failed to listen on pod info socket: %v", err)
	}
	if err := os.Chmod(socketpath, 0600); err != nil {
		_ = l.Close()
		klog.Exit("failed to listen on pod info socket: %v", err)
	}

	server.SetKeepAlivesEnabled(false)
	go utilwait.Forever(func() {
		if err := server.Serve(l); err != nil {
			utilruntime.HandleError(fmt.Errorf("CNI server Serve() failed: %v", err))
		}
	}, 0)

	select {

	}
}

func socketPath(rundir string) string {
	return filepath.Join(rundir, serverSocketName)
}
