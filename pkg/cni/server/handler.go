package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/maiqueb/ovn-cni/pkg/cni"
	"github.com/maiqueb/ovn-cni/pkg/ovn"
	"github.com/maiqueb/ovn-cni/pkg/ovs"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/gorilla/mux"
	"k8s.io/klog"
)

// KubeAPIAuth contains information necessary to create a Kube API client
type KubeAPIAuth struct {
	// Kubeconfig is the path to a kubeconfig
	Kubeconfig string `json:"kubeconfig,omitempty"`
	// KubeAPIServer is the URL of a Kubernetes API server (not required if kubeconfig is given)
	KubeAPIServer string `json:"kube-api-server,omitempty"`
	// KubeAPIToken is a Kubernetes API token (not required if kubeconfig is given)
	KubeAPIToken string `json:"kube-api-token,omitempty"`
	// KubeCAData is the Base64-ed Kubernetes API CA certificate data (not required if kubeconfig is given)
	KubeCAData string `json:"kube-ca-data,omitempty"`
}

type Server struct {
	http.Server
	requestFunc cniRequestFunc
	rundir      string
	useOVSExternalIDs int32
	kubeAuth    *KubeAPIAuth
}

// Explicit type for CNI commands the server handles
type command string

type PodRequest struct {
	// The CNI command of the operation
	Command command
	// kubernetes namespace name
	PodNamespace string
	// kubernetes pod name
	PodName string
	// kubernetes pod UID
	PodUID string
	// kubernetes container ID
	SandboxID string
	// kernel network namespace path
	Netns string
	// Interface name to be configured
	IfName string
	// CNI conf obtained from stdin conf
	CNIConf *types.NetConf
	// Timestamp when the request was started
	timestamp time.Time
	// ctx is a context tracking this request's lifetime
	ctx context.Context
	// cancel should be called to cancel this request
	cancel context.CancelFunc
}

func (pr *PodRequest) cmdAdd() ([]byte, error) {
	namespace := pr.PodNamespace
	podName := pr.PodName
	if namespace == "" || podName == "" {
		return nil, fmt.Errorf("required CNI variable missing")
	}

	portName := ovn.GeneratePortName(namespace, podName, pr.CNIConf.Name)

	//var ipConfig *current.IPConfig
	var portCIDR *net.IPNet

	netns, err := ns.GetNS(pr.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open netns %q: %v", pr.Netns, err)
	}
	defer netns.Close()

	klog.Infof("Setting up veth pair for iface: %s", pr.IfName)
	hostIface, contIface, err := cni.Setup(netns, pr.IfName, 0, portCIDR)
	if err != nil {
		return nil, err
	}

	klog.Infof("Adding OVS port for host iface: %s, port %s", hostIface, portName)
	if err := ovs.CreatePort(hostIface, portName, ""); err != nil {
		return nil, err
	}

	responseBytes, err := json.Marshal(&Response{Result: &current.Result{
		Interfaces: []*current.Interface{hostIface, contIface},
		IPs:        nil,
	}})

	if err != nil {
		return nil, fmt.Errorf("failed to marshal pod request response: %v", err)
	}

	return responseBytes, nil
}

type cniRequestFunc func(request *PodRequest) ([]byte, error)

// Request sent to the Server by the OVN CNI plugin
type Request struct {
	// CNI environment variables, like CNI_COMMAND and CNI_NETNS
	Env map[string]string `json:"env,omitempty"`
	// CNI configuration passed via stdin to the CNI plugin
	Config []byte `json:"config,omitempty"`
}

type Response struct {
	Result    *current.Result
}

// NewCNIServer creates and returns a new Server object which will listen on a socket in the given path
func NewCNIServer(rundir string) (*Server, error) {
	router := mux.NewRouter()

	s := &Server{
		Server: http.Server{
			Handler: router,
		},
		rundir:            rundir,
		requestFunc: HandleCNIRequest,
	}

	router.NotFoundHandler = http.HandlerFunc(http.NotFound)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.handleCNIRequest(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("%v", err), http.StatusBadRequest)
			return
		}

		// Empty response JSON means success with no body
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(result); err != nil {
			klog.Warningf("Error writing HTTP response: %v", err)
		}
	}).Methods("POST")

	return s, nil
}

func (s *Server) handleCNIRequest(r *http.Request) ([]byte, error) {
	var cr Request
	b, _ := ioutil.ReadAll(r.Body)
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, err
	}
	req, err := cniRequestToPodRequest(&cr)
	if err != nil {
		return nil, err
	}
	defer req.cancel()

	result, err := s.requestFunc(req)
	if err != nil {
		// Prefix error with request information for easier debugging
		return nil, fmt.Errorf("%s %v", req, err)
	}
	return result, nil
}

func cniRequestToPodRequest(cr *Request) (*PodRequest, error) {
	cmd, ok := cr.Env["CNI_COMMAND"]
	if !ok {
		return nil, fmt.Errorf("unexpected or missing CNI_COMMAND")
	}

	req := &PodRequest{
		Command: command(cmd),
	}

	req.SandboxID, ok = cr.Env["CNI_CONTAINERID"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_CONTAINERID")
	}
	req.Netns, ok = cr.Env["CNI_NETNS"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_NETNS")
	}

	req.IfName, ok = cr.Env["CNI_IFNAME"]
	if !ok {
		req.IfName = "eth0"
	}

	cniArgs, err := gatherCNIArgs(cr.Env)
	if err != nil {
		return nil, err
	}

	req.PodNamespace, ok = cniArgs["K8S_POD_NAMESPACE"]
	if !ok {
		return nil, fmt.Errorf("missing K8S_POD_NAMESPACE")
	}

	req.PodName, ok = cniArgs["K8S_POD_NAME"]
	if !ok {
		return nil, fmt.Errorf("missing K8S_POD_NAME")
	}

	// UID may not be passed by all runtimes yet. Will be passed
	// by CRIO 1.20+ and containerd 1.5+ soon.
	// CRIO 1.20: https://github.com/cri-o/cri-o/pull/5029
	// CRIO 1.21: https://github.com/cri-o/cri-o/pull/5028
	// CRIO 1.22: https://github.com/cri-o/cri-o/pull/5026
	// containerd 1.6: https://github.com/containerd/containerd/pull/5640
	// containerd 1.5: https://github.com/containerd/containerd/pull/5643
	req.PodUID = cniArgs["K8S_POD_UID"]

	conf, err := ReadCNIConfig(cr.Config)
	if err != nil {
		return nil, fmt.Errorf("broken stdin args")
	}

	req.CNIConf = &conf.NetConf
	req.timestamp = time.Now()
	req.ctx, req.cancel = context.WithTimeout(context.Background(), time.Minute)
	return req, nil
}

func gatherCNIArgs(env map[string]string) (map[string]string, error) {
	cniArgs, ok := env["CNI_ARGS"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_ARGS: '%s'", env)
	}

	mapArgs := make(map[string]string)
	for _, arg := range strings.Split(cniArgs, ";") {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid CNI_ARG '%s'", arg)
		}
		mapArgs[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return mapArgs, nil
}

// ReadCNIConfig unmarshals a CNI JSON config into an NetConf structure
func ReadCNIConfig(bytes []byte) (*NetConf, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, err
	}
	if conf.RawPrevResult != nil {
		if err := version.ParsePrevResult(&conf.NetConf); err != nil {
			return nil, err
		}
	}
	return conf, nil
}

// CNIAdd is the command representing add operation for a new pod
const CNIAdd command = "ADD"

// CNIUpdate is the command representing update operation for an existing pod
const CNIUpdate command = "UPDATE"

// CNIDel is the command representing delete operation on a pod that is to be torn down
const CNIDel command = "DEL"

// CNICheck is the command representing check operation on a pod
const CNICheck command = "CHECK"

func HandleCNIRequest(request *PodRequest) ([]byte, error) {
	var result []byte
	var err error

	klog.Infof("%s %s starting CNI request %+v", request, request.Command, request)
	switch request.Command {
	case CNIAdd:
		result, err = request.cmdAdd()
	case CNIDel:
		klog.Infof("TODO")
	case CNICheck:
		klog.Infof("TODO")
	default:
	}
	klog.Infof("%s %s finished CNI request %+v, result %q, err %v", request, request.Command, request, string(result), err)

	if err != nil {
		// Prefix errors with request info for easier failure debugging
		return nil, fmt.Errorf("%s %v", request, err)
	}
	return result, nil
}
