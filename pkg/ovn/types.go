package ovn

type LogicalSwitch struct {
	UUID   string            `ovsdb:"_uuid"` // _uuid tag is mandatory
	Name   string            `ovsdb:"name"`
	Ports  []string          `ovsdb:"ports"`
	Config map[string]string `ovsdb:"other_config"`
}

type LogicalSwitchPort struct {
	UUID   string            `ovsdb:"_uuid"` // _uuid tag is mandatory
	Name   string            `ovsdb:"name"`
	Ports  []string          `ovsdb:"ports"`
	Config map[string]string `ovsdb:"other_config"`
}
