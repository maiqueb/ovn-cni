package controller

import (
	"encoding/json"
	"fmt"
	v1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/maiqueb/ovn-cni/pkg/api"
	"github.com/maiqueb/ovn-cni/pkg/config"
	"github.com/maiqueb/ovn-cni/pkg/ovn"
	"github.com/ovn-org/libovsdb/ovsdb"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	clientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	informers "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/informers/externalversions/k8s.cni.cncf.io/v1"
)

const (
	cncfNetworksKey      = "k8s.v1.cni.cncf.io/networks"
	cncfNetworkStatusKey = "k8s.v1.cni.cncf.io/networks-status"
	controllerName       = "k8s-net-attach-def-controller"
)

// NetworkController is the controller implementation for handling net-attach-def resources and other objects using them
type NetworkController struct {
	k8sClientSet          kubernetes.Interface
	netAttachDefClientSet clientset.Interface

	netAttachDefsSynced cache.InformerSynced
	podsSynced          cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder

	ovnClient ovn.NorthClient
}

// NewNetworkController returns new NetworkController instance
func NewNetworkController(
	k8sClientSet kubernetes.Interface,
	podInformer coreinformers.PodInformer,
	netAttachDefClientSet clientset.Interface,
	netAttachDefInformer informers.NetworkAttachmentDefinitionInformer,
	ovnConfig config.OvnConfig) (*NetworkController, error) {

	klog.V(3).Info("creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: k8sClientSet.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerName})

	ovnClient, err := ovn.NewOVNNBClient(ovnConfig)
	if err != nil {
		klog.Errorf("failed creating the OVN north client: %v", err)
		return nil, err
	}
	networkController := &NetworkController{
		k8sClientSet:          k8sClientSet,
		netAttachDefClientSet: netAttachDefClientSet,
		netAttachDefsSynced:   netAttachDefInformer.Informer().HasSynced,
		podsSynced:            podInformer.Informer().HasSynced,
		workqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"secondary-ovn-networks"),
		recorder:  recorder,
		ovnClient: ovnClient,
	}

	netAttachDefInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    networkController.handleNetAttachDefAddEvent,
		DeleteFunc: networkController.handleNetAttachDefDeleteEvent,
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    networkController.handleNewPod,
		UpdateFunc: networkController.handleUpdatePod,
		DeleteFunc: networkController.handleDeletePod,
	})

	return networkController, nil
}

func (c *NetworkController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *NetworkController) processNextWorkItem() bool {
	key, shouldQuit := c.workqueue.Get()
	if shouldQuit {
		return false
	}
	defer c.workqueue.Done(key)

	return true
}

func (c *NetworkController) handleServiceEvent(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

func (c *NetworkController) handlePodEvent(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	// if no network annotation discard
	_, ok = pod.GetAnnotations()[cncfNetworksKey]
	if !ok {
		klog.V(4).Info("skipping pod event: network annotations missing")
		return
	}

}

func (c *NetworkController) handleNetAttachDefAddEvent(obj interface{}) {
	klog.V(4).Infof("add net-attach-def event: %v", obj)
	nad, ok := obj.(*v1.NetworkAttachmentDefinition)
	if !ok {
		return
	}

	ovnNet, err := getOvnSecondaryNetworkInfo(*nad)
	if err != nil {
		return
	}

	operations, err := c.ovnClient.CreateLogicalSwitch(nad.GetName(), nad.GetNamespace(), *ovnNet)
	if err != nil {
		klog.Errorf("failed to generate logical switch for network: %s. Reason: %v", nad.GetName(), err)
	}

	if err := c.ovnClient.CommitTransactions(operations); err != nil {
		klog.Errorf("%w", err)
		return
	}
}

func (c *NetworkController) handleNetAttachDefDeleteEvent(obj interface{}) {
	klog.V(4).Infof("remove net-attach-def event: %v", obj)
	nad, ok := obj.(*v1.NetworkAttachmentDefinition)
	if !ok {
		return
	}

	_, err := getOvnSecondaryNetworkInfo(*nad)
	if err != nil {
		return
	}

	operations, err := c.ovnClient.RemoveLogicalSwitch(nad.GetName(), nad.GetNamespace())
	if err != nil {
		klog.Errorf("failed to remove logical switch for network: %s. Reason: %v", nad.GetName(), err)
	}

	if err := c.ovnClient.CommitTransactions(operations); err != nil {
		klog.Errorf("%w", err)
		return
	}
}

func (c *NetworkController) handleNewPod(obj interface{}) {
	//klog.V(4).Infof("add pod event: %v", obj)
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	podSecondaryNetworks, err := getPodOvnSecondaryNetworks(pod.GetAnnotations())
	if err != nil {
		return
	}
	klog.Infof("pod %s has networks: %v", pod.GetName(), podSecondaryNetworks)
	var operations []ovsdb.Operation
	for _, networkName := range podSecondaryNetworks {
		klog.Infof("going to create LSP for pod %s on network %s", pod.GetName(), networkName)
		createLspOperations, err := c.ovnClient.CreateLogicalSwitchPort(pod.GetName(), pod.GetNamespace(), networkName)
		if err != nil {
			klog.Errorf("failed to create logical switch port for pod: %s. Reason: %v", pod.GetName(), err)
		}
		operations = append(operations, createLspOperations...)
	}

	if err := c.ovnClient.CommitTransactions(operations); err != nil {
		klog.Errorf("%w", err)
		return
	}
}

func (c *NetworkController) handleUpdatePod(oldObj interface{}, newObj interface{}) {
	//klog.V(4).Infof("update pod event: oldObj: %v, newObj: %v", oldObj, newObj)
	_, ok := oldObj.(*corev1.Pod)
	if !ok {
		return
	}

	_, ok = newObj.(*corev1.Pod)
	if !ok {
		return
	}
}

func (c *NetworkController) handleDeletePod(obj interface{}) {
	//klog.V(4).Infof("remove pod event: %v", obj)
	_, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
}

// Start runs worker thread after performing cache synchronization
func (c *NetworkController) Start(stopChan <-chan struct{}) {
	klog.V(4).Infof("starting network controller")
	defer c.workqueue.ShutDown()

	if ok := cache.WaitForCacheSync(stopChan, c.netAttachDefsSynced); !ok {
		klog.Fatalf("failed waiting for caches to sync")
	}

	go wait.Until(c.worker, time.Second, stopChan)

	<-stopChan
	klog.V(4).Infof("shutting down network controller")
	return
}

func getOvnSecondaryNetworkInfo(nad v1.NetworkAttachmentDefinition) (*api.OvnSecondaryNetwork, error) {
	ovnCniConfig := &api.OvnSecondaryNetwork{}

	if err := json.Unmarshal([]byte(nad.Spec.Config), ovnCniConfig); err != nil {
		klog.Errorf("could not unmarshall net-attach-def data: %v", err)
		return nil, err
	}
	klog.Errorf("NAD type: %s", ovnCniConfig.Type)
	if ovnCniConfig.Type == "ovn-cni" {
		return ovnCniConfig, nil
	}
	return nil, fmt.Errorf("not an ovn secondary network")
}

func getPodOvnSecondaryNetworks(podAnnotations map[string]string) ([]string, error) {
	networkAnnotationsString, ok := podAnnotations[cncfNetworksKey]
	if !ok {
		klog.V(4).Info("skipping pod event: network annotations missing")
		return nil, fmt.Errorf("no network annotations found on pod: %v", podAnnotations)
	}

	var secondaryNetworks []string
	for _, item := range strings.Split(networkAnnotationsString, ",") {
		// Remove leading and trailing whitespace.
		item = strings.TrimSpace(item)

		//// Parse network name (i.e. <namespace>/<network name>@<ifname>)
		//netNsName, networkName, err := parsePodNetworkObjectName(item)
		//if err != nil {
		//	return nil, fmt.Errorf("parsePodNetworkAnnotation: %v", err)
		//}
		secondaryNetworks = append(secondaryNetworks, item)
	}

	//var networkSelectionElements []v1.NetworkSelectionElement
	//if err := json.Unmarshal([]byte(networkAnnotationsString), &networkSelectionElements); err != nil {
	//	return nil, err
	//}
	//
	//var secondaryNetworks []string
	//for _, networkSelectionElement := range networkSelectionElements {
	//	klog.Infof("network selection element: %+v", networkSelectionElement)
	//	secondaryNetworks = append(secondaryNetworks, networkSelectionElement.Name)
	//}
	return secondaryNetworks, nil
}

func parsePodNetworkObjectName(podNetwork string) (string, string, error) {
	arr := strings.Split(podNetwork, "/")
	if len(arr) == 2 {
		return arr[0], arr[1], nil
	} else {
		return "", "", fmt.Errorf("wrong inpuit")
	}
}
