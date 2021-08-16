package ovn

import "fmt"

const ovnSecondaryNetsPrefix = "ovn_secondary"

func GeneratePortName(namespace string, podName string, ovnNetworkName string) string {
	return fmt.Sprintf("%s_%s_%s_%s", ovnSecondaryNetsPrefix, namespace, podName, ovnNetworkName)
}

func GenerateOvnNetworkName(namespace string, ovnNetworkName string) string {
	return fmt.Sprintf("%s_%s_%s", ovnSecondaryNetsPrefix, namespace, ovnNetworkName)
}
