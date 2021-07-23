package controller

import (
	v1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/maiqueb/ovn-cni/pkg/config"
	"github.com/maiqueb/ovn-cni/pkg/ovn"
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
	podsSynced cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder

	ovnClient ovn.NorthClient
}

// NewNetworkController returns new NetworkController instance
func NewNetworkController(
	k8sClientSet kubernetes.Interface,
	podInformer coreinformers.PodInformer,
	netAttachDefClientSet clientset.Interface,
	netAttachDefInformer informers.NetworkAttachmentDefinitionInformer) *NetworkController {

	klog.V(3).Info("creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: k8sClientSet.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerName})

	ovnConfig, err := config.InitConfigWithPath("/etc/ovn-cni.conf")
	if err != nil {
		klog.V(3).Info("failed reading OVN configuration")
		return nil
	}
	ovnClient, err := ovn.NewOVNNBClient(*ovnConfig)
	if err != nil {
		klog.V(3).Info("failed creating the OVN north client")
		return nil
	}
	NetworkController := &NetworkController{
		k8sClientSet:          k8sClientSet,
		netAttachDefClientSet: netAttachDefClientSet,
		netAttachDefsSynced:   netAttachDefInformer.Informer().HasSynced,
		podsSynced:            podInformer.Informer().HasSynced,
		workqueue:             workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"secondary-ovn-networks"),
		recorder:              recorder,
		ovnClient:             ovnClient,
	}

	netAttachDefInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: NetworkController.handleNetAttachDefAddEvent,
		DeleteFunc: NetworkController.handleNetAttachDefDeleteEvent,
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: NetworkController.handleNewPod,
		DeleteFunc: NetworkController.handleDeletePod,
	})

	return NetworkController
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
	nad, ok := obj.(*v1.NetworkAttachmentDefinition)
	if !ok {
		return
	}
	operations, err := c.ovnClient.CreateLogicalSwitch(nad.GetName())
	if err != nil {
		klog.Errorf("failed to generate logical switch for network: %s", nad.GetName())
	}

	if err := c.ovnClient.CommitTransactions(operations); err != nil {
		klog.Errorf("%w", err)
		return
	}
}

func (c *NetworkController) handleNetAttachDefDeleteEvent(obj interface{}) {

}

func (c *NetworkController) handleNewPod(obj interface{}) {
}

func (c *NetworkController) handleDeletePod(obj interface{}) {

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