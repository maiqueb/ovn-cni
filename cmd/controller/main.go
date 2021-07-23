package main

import (
	"flag"
	"os"
	"os/signal"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	clientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	sharedInformers "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/informers/externalversions"

	"github.com/maiqueb/ovn-cni/pkg/config"
	"github.com/maiqueb/ovn-cni/pkg/controller"
)

var (
	master     string
	kubeconfig string
	ovnNorth   string
	ovsBridge  string
	ovnContainerName string
	ovsContainerName string

	// defines default resync period between k8s API server and controller
	syncPeriod = time.Second * 5
)

func main() {
	flag.StringVar(&master, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Required if out-of-cluster.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Required if out-of-cluster.")
	flag.StringVar(&ovnNorth, "ovnaddr", "", "The OVN-NB address. Required.")
	flag.StringVar(&ovsBridge, "ovsbridge", "br-int", "The OVS bridge to use.")
	flag.StringVar(&ovnContainerName, "ovncontainer", "ovnkube-node", "The OVN north container. Mandatory with a containerized deployment.")
	flag.StringVar(&ovsContainerName, "ovscontainer", "ovs-daemons", "The OVS container. Mandatory with a containerized deployment.")

	klog.InitFlags(nil)
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags(master, kubeconfig)
	if err != nil {
		klog.Fatalf("error building kubeconfig: %s", err.Error())
	}

	k8sClientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("error creating kubernetes clientset: %s", err.Error())
	}

	netAttachDefClientSet, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("error creating net-attach-def clientset: %s", err.Error())
	}

	netAttachDefInformerFactory := sharedInformers.NewSharedInformerFactory(netAttachDefClientSet, syncPeriod)
	k8sInformerFactory := informers.NewSharedInformerFactory(k8sClientSet, syncPeriod)

	networkController, err := controller.NewNetworkController(
		k8sClientSet,
		k8sInformerFactory.Core().V1().Pods(),
		netAttachDefClientSet,
		netAttachDefInformerFactory.K8sCniCncfIo().V1().NetworkAttachmentDefinitions(),
		newOvnConfig())
	if err != nil {
		os.Exit(-1)
	}

	stopChan := make(chan struct{})
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		close(stopChan)
		<-c
		os.Exit(1)
	}()

	netAttachDefInformerFactory.Start(stopChan)
	k8sInformerFactory.Start(stopChan)
	networkController.Start(stopChan)
}

func newOvnConfig() config.OvnConfig {
	return config.OvnConfig{
		OvsBridge:    ovsBridge,
		Address:      ovnNorth,
		OvnContainer: ovnContainerName,
		OvsContainer: ovsContainerName,
	}
}
