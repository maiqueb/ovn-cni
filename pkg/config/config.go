package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/gcfg.v1"
)

type Config struct {
	OVNConfig OvnConfig `gcfg:"ovn"`
}

type OvnConfig struct {
	OvsBridge    string `gcfg:"ovs-bridge"`
	Address      string `gcfg:"address"`
	PrivKey      string `gcfg:"privkey"`
	Cert         string `gcfg:"cert"`
	CACert       string `gcfg:"cacert"`
	OvnContainer string `gcfg:"ovn-container-name"`
	OvsContainer string `gcfg:"ovs-container-name"`
}

func SchemeIsUnix(ovnConfig OvnConfig) bool {
	return strings.HasPrefix(ovnConfig.Address, "unix") || len(ovnConfig.Address) == 0
}

func SchemeIsTCP(ovnConfig OvnConfig) bool {
	return strings.HasPrefix(ovnConfig.Address, "tcp")
}

func SchemeIsSSL(ovnConfig OvnConfig) bool {
	return strings.HasPrefix(ovnConfig.Address, "ssl")
}

func InitConfigWithPath(configFile string) (*OvnConfig, error) {
	f, err := os.Open(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open Config file %s: %v", configFile, err)
	}
	defer f.Close()

	var cfg Config
	if err = gcfg.ReadInto(&cfg, f); err != nil {
		return nil, fmt.Errorf("failed to parse Config file %s: %v", f.Name(), err)
	}

	ovnConfig := &cfg.OVNConfig
	if ovnConfig.OvsBridge == "" {
		ovnConfig.OvsBridge = "br-int"
	}

	switch {
	case SchemeIsUnix(*ovnConfig) || SchemeIsTCP(*ovnConfig):
		if ovnConfig.PrivKey != "" || ovnConfig.Cert != "" || ovnConfig.CACert != "" {
			return nil, fmt.Errorf("certificate or key given; perhaps you mean to use the 'ssl' scheme")
		}
	case SchemeIsSSL(*ovnConfig):
		if !pathExists(ovnConfig.PrivKey) {
			return nil, fmt.Errorf("private key file %s not found", ovnConfig.PrivKey)
		}
		if !pathExists(ovnConfig.Cert) {
			return nil, fmt.Errorf("certificate file %s not found", ovnConfig.Cert)
		}
		if !pathExists(ovnConfig.CACert) {
			return nil, fmt.Errorf("CA certificate file %s not found", ovnConfig.CACert)
		}
	}

	return ovnConfig, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}
