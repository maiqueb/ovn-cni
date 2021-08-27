package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	cniserver "github.com/maiqueb/ovn-cni/pkg/cni/server"
	"io/ioutil"
	"k8s.io/klog"
	"net"
	"net/http"
	"os"
	"strings"
)

type Plugin struct {
	socketPath string
}

// Request sent to the Server by the OVN CNI plugin
type Request struct {
	// CNI environment variables, like CNI_COMMAND and CNI_NETNS
	Env map[string]string `json:"env,omitempty"`
	// CNI configuration passed via stdin to the CNI plugin
	Config []byte `json:"config,omitempty"`
}

// NewCNIPlugin creates the internal Plugin object
func NewCNIPlugin(socketPath string) *Plugin {
	if len(socketPath) == 0 {
		socketPath = cniserver.SocketPath(cniserver.DefaultRundir)
	}
	return &Plugin{socketPath: socketPath}
}

// Create and fill a Request with this Plugin's environment and stdin which
// contain the CNI variables and configuration
func newCNIRequest(args *skel.CmdArgs) *Request {
	envMap := make(map[string]string)
	for _, item := range os.Environ() {
		idx := strings.Index(item, "=")
		if idx > 0 {
			envMap[strings.TrimSpace(item[:idx])] = item[idx+1:]
		}
	}

	return &Request{
		Env:    envMap,
		Config: args.StdinData,
	}
}

// Send a CNI request to the CNI server via JSON + HTTP over a root-owned unix socket,
// and return the result
func (p *Plugin) doCNI(url string, req interface{}) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CNI request %v: %v", req, err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(proto, addr string) (net.Conn, error) {
				return net.Dial("unix", p.socketPath)
			},
		},
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to send CNI request: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read CNI result: %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CNI request failed with status %v: '%s'", resp.StatusCode, string(body))
	}

	return body, nil
}

func (p *Plugin) CmdAdd(args *skel.CmdArgs) error {
	var err error

	// read the config stdin args to obtain cniVersion
	conf, errC := cniserver.ReadCNIConfig(args.StdinData)
	if errC != nil {
		err = fmt.Errorf("invalid stdin args %v", errC)
		return err
	}

	req := newCNIRequest(args)

	body, err := p.doCNI("http://dummy/", req)
	if err != nil {
		klog.Error(err.Error())
		return err
	}

	response := &cniserver.Response{}
	if err = json.Unmarshal(body, response); err != nil {
		err = fmt.Errorf("failed to unmarshal response '%s': %v", string(body), err)
		klog.Error(err.Error())
		return err
	}

	return types.PrintResult(response.Result, conf.CNIVersion)
}

func main()  {
	p := NewCNIPlugin("")
	skel.PluginMain(
		p.CmdAdd,
		func(_ *skel.CmdArgs) error {
			return nil
		},
		func(_ *skel.CmdArgs) error {
			return nil
		}, version.All, "An OVN cni plugin for secondary networks")
}
