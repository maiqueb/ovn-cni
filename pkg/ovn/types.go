package ovn

type LogicalSwitch struct {
	UUID   string            `ovsdb:"_uuid"` // _uuid tag is mandatory
	Name   string            `ovsdb:"name"`
	Ports  []string          `ovsdb:"ports"`
	Config map[string]string `ovsdb:"other_config"`
}

type LogicalSwitchPort struct {
	UUID             string            `ovsdb:"_uuid"` // _uuid tag is mandatory
	Name             string            `ovsdb:"name"`
	Type             string            `ovsdb:"type"`
	Options          map[string]string `ovsdb:"options"`
	Addresses        []string          `ovsdb:"addresses"`
	DynamicAddresses []string          `ovsdb:"dynamic_addresses"`
	PortSecurity     []string          `ovsdb:"port_security"`
}
