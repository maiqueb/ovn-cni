package api

import "github.com/containernetworking/cni/pkg/types"

type OvnSecondaryNetwork struct {
	types.NetConf
	Subnet string `json:"subnet"`
	HasExternalConnectivity bool `json:"has-external-connectivity,omitempty"`
}
