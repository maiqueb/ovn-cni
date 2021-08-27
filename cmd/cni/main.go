package main

import (
	"encoding/json"
	"fmt"
	"github.com/maiqueb/ovn-cni/pkg/ovs"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/maiqueb/ovn-cni/pkg/api"
	"github.com/maiqueb/ovn-cni/pkg/cni"
	"github.com/maiqueb/ovn-cni/pkg/ovn"
)

type EnvArgs struct {
	types.CommonArgs
	IP                types.UnmarshallableString `json:"ip,omitempty"`
	MAC               types.UnmarshallableString `json:"mac,omitempty"`
	K8S_POD_NAMESPACE types.UnmarshallableString `json:"k8s_pod_namespace"`
	K8S_POD_NAME      types.UnmarshallableString `json:"k8s_pod_name"`
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "An OVN cni plugin for secondary networks")
}

func cmdAdd(args *skel.CmdArgs) error {
	n, cniVersion, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	envArgs, err := getPodDetails(args.Args)
	if err != nil {
		return err
	}

	portName := ovn.GeneratePortName(string(envArgs.K8S_POD_NAMESPACE), string(envArgs.K8S_POD_NAME), n.Name)

	var ipConfig *current.IPConfig
	var portCIDR *net.IPNet

	hostIface, contIface, err := cni.Setup(netns, args.IfName, 0, portCIDR)
	if err != nil {
		return err
	}

	if err := ovs.CreatePort(hostIface, portName, ""); err != nil {
		return err
	}

	result := &current.Result{
		Interfaces: []*current.Interface{hostIface, contIface},
	}
	if ipConfig != nil {
		result.IPs = append(result.IPs, ipConfig)
	}
	return types.PrintResult(result, cniVersion)
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func cmdDel(args *skel.CmdArgs) error {
	return nil
}

func loadConf(bytes []byte) (*api.CNIOvnSecondaryNetwork, string, error) {
	n := &api.OvnSecondaryNetwork{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}
	if err := version.ParsePrevResult(&n.NetConf); err != nil {
		return nil, "", err
	}
	if n.Name == "" {
		return nil, "", fmt.Errorf("a network name is required")
	}
	var subnet *net.IPNet
	if n.Subnet != "" {
		var err error
		_, subnet, err = net.ParseCIDR(n.Subnet)
		if err != nil || subnet == nil {
			return nil, "", fmt.Errorf("failed to parse subnet %q: %v", n.Subnet, err)
		}
		return &api.CNIOvnSecondaryNetwork{
			OvnSecondaryNetwork: *n,
			Subnet:              *subnet,
		}, n.CNIVersion, nil
	}

	return &api.CNIOvnSecondaryNetwork{
		OvnSecondaryNetwork: *n,
	}, n.CNIVersion, nil
}

func getPodDetails(args string) (*EnvArgs, error) {
	e := &EnvArgs{}
	err := types.LoadArgs(args, e)
	if err != nil {
		return nil, err
	}
	if e.K8S_POD_NAMESPACE == "" {
		return nil, fmt.Errorf("missing K8S_POD_NAMESPACE")
	}
	if e.K8S_POD_NAME == "" {
		return nil, fmt.Errorf("missing K8S_POD_NAME")
	}
	return e, nil
}

func parsePodIP(podIP string, subnet *net.IPNet) (net.IP, error) {
	portIP := net.ParseIP(podIP)
	if portIP == nil && podIP != "" {
		return nil, fmt.Errorf("invalid pod IP %q", podIP)
	}
	if subnet != nil && podIP != "" && !subnet.Contains(portIP) {
		return nil, fmt.Errorf("switch subnet %q does not contain requested pod IP %q", subnet.String(), podIP)
	}
	return portIP, nil
}
