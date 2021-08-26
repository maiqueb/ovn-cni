package cni

import (
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

//func Setup(netns ns.NetNS, ifName string, mtu int, macAddr string, ipAddr *net.IPNet) (*current.Interface, *current.Interface, error) {
func Setup(netns ns.NetNS, ifName string, mtu int, ipAddr *net.IPNet) (*current.Interface, *current.Interface, error) {
	hostIface := &current.Interface{}
	contIface := &current.Interface{}

	if err := netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, contVeth, err := ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}

		//addr, err := net.ParseMAC(macAddr)
		//if err != nil {
		//	return fmt.Errorf("invalid MAC address %q: %v", macAddr, err)
		//}
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to get %q: %v", ifName, err)
		}
		err = netlink.LinkSetDown(link)
		if err != nil {
			return fmt.Errorf("failed to set %q down: %v", ifName, err)
		}
		//err = netlink.LinkSetHardwareAddr(link, addr)
		//if err != nil {
		//	return fmt.Errorf("failed to set %q address to %q: %v", ifName, macAddr, err)
		//}
		err = netlink.LinkSetUp(link)
		if err != nil {
			return fmt.Errorf("failed to set %q up: %v", ifName, err)
		}

		if ipAddr != nil {
			if err = netlink.AddrAdd(link, &netlink.Addr{IPNet: ipAddr}); err != nil {
				return fmt.Errorf("failed to add IP addr %v to %q: %v", ipAddr, ifName, err)
			}
		}

		hostIface.Name = hostVeth.Name
		hostIface.Mac = hostVeth.HardwareAddr.String()
		contIface.Name = contVeth.Name
		//contIface.Mac = macAddr
		contIface.Sandbox = netns.Path()
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return hostIface, contIface, nil
}
