package ovn

import (
	"context"
	"github.com/maiqueb/ovn-cni/pkg/config"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog"
)

const logicalSwitchTableName = "Logical_Switch"

type NorthClient struct {
	client client.Client
	model  model.Model
}

func NewOVNNBClient(ovnConfig config.OvnConfig) (NorthClient, error) {
	dbModel, err := model.NewDBModel("OVN_Northbound", map[string]model.Model{
		logicalSwitchTableName: &LogicalSwitch{},
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

func (nc NorthClient) CreateLogicalSwitch(name string) ([]ovsdb.Operation, error) {
	return nc.client.Create(&LogicalSwitch{Name: name})
}

func (nc NorthClient) RemoveLogicalSwitch(name string) ([]ovsdb.Operation, error) {
	ls := &LogicalSwitch{}
	return nc.client.Where(ls, model.Condition{
		Field:    &ls.Name,
		Function: ovsdb.ConditionEqual,
		Value:    name,
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
