package ovn

import (
	"context"
	"github.com/maiqueb/ovn-cni/pkg/config"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog"
)

type NorthClient struct {
	client client.Client
	model  model.Model
}

func NewOVNNBClient(ovnConfig config.OvnConfig) (NorthClient, error) {
	dbModel, _ := model.NewDBModel("OVN_Northbound", map[string]model.Model{
		"Logical_Switch": &LogicalSwitch{},
	})

	ovnNBClient, err := client.NewOVSDBClient(dbModel, client.WithEndpoint(ovnConfig.Address))
	if err != nil {
		return NorthClient{}, err
	}
	if err := ovnNBClient.Connect(context.Background()); err != nil {
		return NorthClient{}, err
	}

	return NorthClient{model: dbModel, client: ovnNBClient}, nil
}

func (nc NorthClient) CreateLogicalSwitch(name string) ([]ovsdb.Operation, error) {
	return nc.client.Create(LogicalSwitch{Name: name})
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
