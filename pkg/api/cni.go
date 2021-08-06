package api

import (
	"github.com/containernetworking/cni/pkg/types"
	"net"
)

type OvnSecondaryNetwork struct {
	types.NetConf
	Subnet                  string `json:"subnet"`
	HasExternalConnectivity bool   `json:"has-external-connectivity,omitempty"`
}

type CNIOvnSecondaryNetwork struct {
	OvnSecondaryNetwork
	Subnet net.IPNet
}
