package ovn

import (
	"context"
	"github.com/maiqueb/ovn-cni/pkg/api"
	"github.com/maiqueb/ovn-cni/pkg/config"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog"
	"math/big"
	"net"
)

const (
	logicalSwitchTableName        = "Logical_Switch"
	logicalSwitchPortTableName    = "Logical_Switch_Port"
	logicalSwitchConfigExcludeIPs = "exclude_ips"
	logicalSwitchConfigSubnet     = "subnet"
)

type NorthClient struct {
	client client.Client
	model  model.Model
}

func NewOVNNBClient(ovnConfig config.OvnConfig) (NorthClient, error) {
	dbModel, err := model.NewDBModel("OVN_Northbound", map[string]model.Model{
		logicalSwitchTableName: &LogicalSwitch{},
		logicalSwitchPortTableName: &LogicalSwitchPort{},
	})
	if err != nil {
		return NorthClient{}, err
	}
	ovnNBClient, err := client.NewOVSDBClient(dbModel, client.WithEndpoint(ovnConfig.Address))
	if err != nil {
		return NorthClient{}, err
	}
	if err := ovnNBClient.Connect(context.Background()); err != nil {
		return NorthClient{}, err
	}

	return NorthClient{model: dbModel, client: ovnNBClient}, nil
}

func (nc NorthClient) CreateLogicalSwitch(networkName string, namespace string, networkConfig api.OvnSecondaryNetwork) ([]ovsdb.Operation, error) {
	lsConfig := map[string]string{}
	if networkConfig.Subnet != "" {
		lsConfig[logicalSwitchConfigSubnet] = networkConfig.Subnet
		lsConfig[logicalSwitchConfigExcludeIPs] = calculateGatewayIP(networkConfig.Subnet).String()
	}
	return nc.client.Create(
		&LogicalSwitch{
			Name:   GenerateOvnNetworkName(namespace, networkName),
			Config: lsConfig,
		})
}

func (nc NorthClient) RemoveLogicalSwitch(networkName string, namespace string) ([]ovsdb.Operation, error) {
	ls := &LogicalSwitch{}
	return nc.client.Where(ls, model.Condition{
		Field:    &ls.Name,
		Function: ovsdb.ConditionEqual,
		Value:    GenerateOvnNetworkName(namespace, networkName),
	}).Delete()
}

func (nc NorthClient) CreateLogicalSwitchPort(podName string, namespace string, switchName string) ([]ovsdb.Operation, error) {
	logicalSwitchPort := &LogicalSwitchPort{
		Name:      GeneratePortName(namespace, podName, switchName),
		UUID:      podName,
		Addresses: []string{"dynamic"},
	}

	createLogicalSwitchOps, err := nc.client.Create(logicalSwitchPort)
	if err != nil {
		return nil, err
	}

	logicalSwitch := &LogicalSwitch{}
	updateLogicalSwitchPortsOps, err := nc.client.Where(logicalSwitch,
		model.Condition{
			Field:    &logicalSwitch.Name,
			Function: ovsdb.ConditionEqual,
			Value:    GenerateOvnNetworkName(namespace, switchName),
		}).Mutate(logicalSwitch,
		model.Mutation{
			Field:   &logicalSwitch.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{podName},
		},
	)
	if err != nil {
		return nil, err
	}
	createLogicalSwitchOps = append(createLogicalSwitchOps, updateLogicalSwitchPortsOps...)
	return createLogicalSwitchOps, nil
}

func (nc NorthClient) UpdateLogicalSwitchPort(name string, mac string, addresses []string) ([]ovsdb.Operation, error) {
	lsp := &LogicalSwitchPort{Name: name}
	if err := nc.client.Get(lsp); err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		lsp.Addresses = []string{"dynamic"}
	} else {
		lsp.Addresses = append([]string{mac}, addresses...)
	}
	return nc.client.Where(lsp, model.Condition{
		Field:    &lsp.Name,
		Function: ovsdb.ConditionEqual,
		Value:    ovnSecondaryNetsPrefix + name,
	}).Update(lsp)
}

func (nc NorthClient) DeleteLogicalSwitchPort(name string) ([]ovsdb.Operation, error) {
	lsp := &LogicalSwitchPort{}
	return nc.client.Where(lsp, model.Condition{
		Field:    &lsp.Name,
		Function: ovsdb.ConditionEqual,
		Value:    ovnSecondaryNetsPrefix + name,
	}).Delete()
}

func (nc NorthClient) CommitTransactions(operations []ovsdb.Operation) error {
	results, err := nc.client.Transact(operations...)
	if err != nil {
		klog.Errorf("failed committing transaction: %w", err)
		return err
	}
	klog.Infof("transaction results: %+v", results)
	return nil
}

func calculateGatewayIP(subnetRange string) net.IP {
	_, subnet, err := net.ParseCIDR(subnetRange)
	if err != nil {
		return nil
	}
	return NextIP(subnet.IP.Mask(subnet.Mask))
}

func NextIP(ip net.IP) net.IP {
	i := ipToInt(ip)
	return intToIP(i.Add(i, big.NewInt(1)))
}

func ipToInt(ip net.IP) *big.Int {
	if v := ip.To4(); v != nil {
		return big.NewInt(0).SetBytes(v)
	}
	return big.NewInt(0).SetBytes(ip.To16())
}

func intToIP(i *big.Int) net.IP {
	return i.Bytes()
}
