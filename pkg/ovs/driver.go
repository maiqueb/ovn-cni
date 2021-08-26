package ovs

import (
	"bytes"
	"fmt"
	"k8s.io/klog"
	"os/exec"
	"strings"

	"github.com/containernetworking/cni/pkg/types/current"
	kexec "k8s.io/utils/exec"
)

const (
	criCli            = "crictl"
	ovsBridge         = "br-int"
	ovsCommandTimeout = 15
	ovsVsctlCommand   = "ovs-vsctl"
)

func CreatePort(hostIface *current.Interface, portName string, portMac string) error {
	ovsArgs := []string{
		"add-port", ovsBridge, hostIface.Name,
		"--", "set", "interface", hostIface.Name,
		//fmt.Sprintf("external_ids:attached_mac=%s", portMac),
		fmt.Sprintf("external_ids:iface-id=%s", portName),
	}
	stdout, stderr, err := RunOVSVsctl(ovsArgs...)
	if err != nil {
		return fmt.Errorf("failed to add port %q to OVS, "+
			"stdout: %q, stderr: %q, error: %v",
			portName, stdout, stderr, err)
	}
	return err
}

type containerizedOvsHelper struct {
	ovsArgs string
}

// Exec runs various OVN and OVS utilities
type execHelper struct {
	exec      kexec.Interface
	vsctlPath string
	containerizedOvsHelper
}

var runner *execHelper

// SetExec validates executable paths and saves the given exec interface
// to be used for running various OVS and OVN utilites
func SetExec(exec kexec.Interface) error {
	var err error

	runner = &execHelper{exec: exec}
	runner.vsctlPath, err = exec.LookPath(ovsVsctlCommand)
	if err != nil {
		return err
	}
	return nil
}

func SetContainerizedExec(exec kexec.Interface, ovsContainerName string) error {
	var err error

	runner = &execHelper{exec: exec}
	ovsVsCtlArgs, err := composeCommandName(ovsVsctlCommand, ovsContainerName)
	if err != nil {
		return err
	}
	runner.vsctlPath = criCli
	runner.ovsArgs = ovsVsCtlArgs

	return nil
}

func composeCommandName(defaultName string, containerName string) (string, error) {
	if containerName != "" {
		ovnNdContainer, err := exec.Command("crictl", "ps", fmt.Sprintf("--name=%s", containerName), "-q").Output()
		if err != nil {
			return "", fmt.Errorf("failed to understand how to contact OVN: %w", err)
		}
		ovnNBCommandPrefix := fmt.Sprintf("exec %s ", strings.TrimSuffix(string(ovnNdContainer), "\n"))
		return ovnNBCommandPrefix + defaultName, nil
	}
	return defaultName, nil
}

func run(cmdPath string, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := runner.exec.Command(cmdPath, args...)
	cmd.SetStdout(stdout)
	cmd.SetStderr(stderr)
	klog.Infof("exec: %s %s", cmdPath, strings.Join(args, " "))
	err := cmd.Run()
	if err != nil {
		klog.Infof("exec: %s %s => %v", cmdPath, strings.Join(args, " "), err)
	}
	return stdout, stderr, err
}

// RunOVSVsctl runs a command via ovs-vsctl.
func RunOVSVsctl(args ...string) (string, string, error) {
	cmdArgs := []string{fmt.Sprintf("--timeout=%d", ovsCommandTimeout)}
	cmdArgs = append(cmdArgs, args...)

	if err := SetContainerizedExec(kexec.New(), "ovs-daemons"); err != nil {
		return "", "", err
	}

	if runner.ovsArgs != "" {
		cmdArgs = append(strings.Split(runner.ovsArgs, " "), cmdArgs...)
	}
	stdout, stderr, err := run(runner.vsctlPath, cmdArgs...)
	return strings.Trim(strings.TrimSpace(stdout.String()), "\""), stderr.String(), err
}
