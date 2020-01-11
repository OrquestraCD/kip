package server

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/elotl/cloud-instance-provider/pkg/api"
	"github.com/elotl/cloud-instance-provider/pkg/nodeclient"
	"github.com/elotl/cloud-instance-provider/pkg/server/cloud"
	"github.com/elotl/cloud-instance-provider/pkg/server/events"
	"github.com/elotl/cloud-instance-provider/pkg/server/nodemanager"
	"github.com/elotl/cloud-instance-provider/pkg/server/registry"
	"github.com/elotl/cloud-instance-provider/pkg/util"
	"github.com/elotl/cloud-instance-provider/pkg/util/conmap"
	"github.com/elotl/cloud-instance-provider/pkg/util/stats"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/resource"
)

// make this configurable
const (
	statusReplyTimeout time.Duration = 90 * time.Second
	podUnboundTooLong  time.Duration = 1 * time.Minute
)

var lastWrongPod map[string]string
var lastWrongNode map[string]string

func init() {
	lastWrongPod = make(map[string]string)
	lastWrongNode = make(map[string]string)
}

type PodController struct {
	podRegistry        *registry.PodRegistry
	logRegistry        *registry.LogRegistry
	metricsRegistry    *registry.MetricsRegistry
	secretLister       registry.SecretLister
	nodeLister         registry.NodeLister
	nodeDispenser      *nodemanager.NodeDispenser
	nodeClientFactory  nodeclient.ItzoClientFactoryer
	events             *events.EventSystem
	cloudClient        cloud.CloudClient
	controllerID       string
	nametag            string
	controlLoopTimer   stats.LoopTimer
	cleanTimer         stats.LoopTimer
	lastStatusReply    *conmap.StringTimeTime
	kubernetesNodeName string
}

type FullPodStatus struct {
	Name             string
	PodIP            string
	UnitStatuses     []api.UnitStatus
	InitUnitStatuses []api.UnitStatus
	ResourceUsage    api.ResourceMetrics
	// If an error occurred, Status will be nil, and Error will contain the
	// error instance.
	Error error
}

func (c *PodController) Start(quit <-chan struct{}, wg *sync.WaitGroup) {
	c.kubernetesNodeName = os.Getenv("NODE_NAME")
	glog.Infof("kubernetes node name: %q", c.kubernetesNodeName)
	if c.kubernetesNodeName == "" {
		glog.Warningf("failed to get NODE_NAME; cell network agent won't run")
	}
	c.registerEventHandlers()
	c.failDispatchingPods()
	go c.ControlLoop(quit, wg)
}

func (c *PodController) registerEventHandlers() {
	// Creates a fast dispatch for pods
	c.events.RegisterHandlerFunc(events.PodCreated, c.podCreated)
	// Useful for kiyot and users updating bare pods
	c.events.RegisterHandlerFunc(events.PodUpdated, c.podUpdated)
}

func (c *PodController) podCreated(e events.Event) error {
	pod := e.Object.(*api.Pod)
	c.schedulePod(pod)
	return nil
}

func (c *PodController) podUpdated(e events.Event) error {
	pod := e.Object.(*api.Pod)
	if pod.Spec.Phase == api.PodRunning &&
		pod.Status.Phase == api.PodRunning {
		err := c.updatePodUnits(pod)
		if err != nil {
			glog.Errorln("Error updating pod units:", err)
		}
	}
	return nil
}

func (c *PodController) Dump() []byte {
	dumpStruct := struct {
		ControlLoopTimer stats.LoopTimer
		CleanTimer       stats.LoopTimer
	}{
		ControlLoopTimer: c.controlLoopTimer,
		CleanTimer:       c.cleanTimer,
	}
	b, err := json.MarshalIndent(dumpStruct, "", "    ")
	if err != nil {
		glog.Errorln("Error dumping data from PodController", err)
		return nil
	}
	return b
}

func (c *PodController) ControlLoop(quit <-chan struct{}, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	glog.Info("starting pod controller")
	ticker := time.NewTicker(5 * time.Second)
	cleanTicker := time.NewTicker(20 * time.Second)
	fullSyncTicker := time.NewTicker(31 * time.Second)
	defer ticker.Stop()
	defer cleanTicker.Stop()
	defer fullSyncTicker.Stop()

	for {
		// prefer quit in case there is a leader election
		select {
		case <-quit:
			glog.Info("Stopping PodController")
			return
		default:
		}
		select {
		case <-ticker.C:
			// todo, see if we can detect ourselves running over time here
			// that would mean that the time between running this section
			// exceeds 2x the c.config.Interval
			c.controlLoopTimer.StartLoop()
			c.checkRunningPodStatus()
			c.ControlPods()
			c.controlLoopTimer.EndLoop()
		case <-fullSyncTicker.C:
			c.SyncRunningPods()
		case <-cleanTicker.C:
			c.cleanTimer.StartLoop()
			c.checkClaimedNodes()
			c.checkRunningPods()
			c.pruneLastStatusReplies()
			c.handleReplyTimeouts()
			c.cleanTimer.EndLoop()
		case <-quit:
			glog.Info("Stopping PodController")
			return
		}
	}
}

// This is a bit of a catch-all for failures. If Milpa fails to
// dispatch a pod or something screws up while running, we call this.
// We ALSO call this when a pod's status changes to api.PodFailed,
// e.g. when executables fail/return non-zero on the pod.  Handling
// both cases in the same way might be a an issue for pods with
// RestartPolicy == api.RestartPolicyNever
func (c *PodController) markFailedPod(pod *api.Pod, startFailure bool, msg string) {
	glog.Infof("Marking pod %s as failed", pod.Name)
	pod.Status.Phase = api.PodFailed
	if startFailure {
		glog.Warningf("Start failure for pod %s", pod.Name)
		pod.Status.StartFailures += 1
		// Note: spotFailure and other items in the status will get
		// overwritten in remedyFailedPod
	}
	_, err := c.podRegistry.UpdatePodStatus(pod, msg)
	if err != nil {
		glog.Errorf("Error updating pod status: %v", err)
	}
	go func() {
		c.savePodLogs(pod)
		glog.Infof("Returning node %s", pod.Status.BoundNodeName)
		c.nodeDispenser.ReturnNode(pod.Status.BoundNodeName, false)
	}()
}

func (c *PodController) loadRegistryCredentials(pod *api.Pod) (map[string]api.RegistryCredentials, error) {
	allCreds := make(map[string]api.RegistryCredentials)
	for _, secretName := range pod.Spec.ImagePullSecrets {
		s, err := c.secretLister.GetSecret(secretName)
		if err != nil {
			return nil, util.WrapError(err, "Could not get secret %s from registry", secretName)
		}
		server := s.Data["server"]
		username, exists := s.Data["username"]
		if !exists {
			return nil, fmt.Errorf(
				"Could not find registry username in secret %s", secretName)
		}
		password, exists := s.Data["password"]
		if !exists {
			return nil, fmt.Errorf(
				"Could not find registry password in secret %s", secretName)
		}
		creds := api.RegistryCredentials{
			Server:   string(server),
			Username: string(username),
			Password: string(password),
		}
		allCreds[string(server)] = creds
		if creds.Username == "" {
			glog.Warningf("Found empty username for image secret %s", secretName)
		}
		if creds.Password == "" {
			// Reviewer: do you think its bad to leak this info?
			glog.Warningf("Found empty password for secret %s", secretName)
		}
	}

	// AWS is different, they require us to authenticate with IAM
	// Do that auth and pass along the username and password
	for i := 0; i < len(pod.Spec.Units); i++ {
		server, _, err := util.ParseImageSpec(pod.Spec.Units[i].Image)
		if err != nil {
			return nil, util.WrapError(err, "Could not parse image spec")
		}
		if strings.HasSuffix(server, "amazonaws.com") {
			creds := allCreds[server]
			if creds.Username != "" || creds.Password != "" {
				// EKS provides a username and password for pulling system
				// container images.
				continue
			}
			username, password, err := c.cloudClient.GetRegistryAuth()
			if err != nil {
				return nil, util.WrapError(err, "Could not get container auth")
			}
			allCreds[server] = api.RegistryCredentials{
				Server:   string(server),
				Username: string(username),
				Password: string(password),
			}
			break
		}
	}
	return allCreds, nil
}

func (c *PodController) resizeVolume(node *api.Node, pod *api.Pod, client nodeclient.NodeClient) error {
	size, err := resource.ParseQuantity(pod.Spec.Resources.VolumeSize)
	if err != nil {
		return err
	}
	sizeGiB := util.ToGiBRoundUp(&size)
	glog.Infof("Pod %s requested volume size of %s on node %s",
		pod.Name, pod.Spec.Resources.VolumeSize, node.Name)
	err, resizePerformed := c.cloudClient.ResizeVolume(node, int64(sizeGiB))
	if err != nil {
		return err
	}
	if resizePerformed {
		// Itzo still needs to take care of enlarging the root partition to
		// span the new, bigger volume.
		glog.Infof("Resized volume on node %s, expanding partition", node.Name)
		return client.ResizeVolume()
	}
	return nil
}

func (c *PodController) loadPodSecrets(pod *api.Pod) (map[string]map[string][]byte, error) {
	secrets := make(map[string]map[string][]byte)
	units := append(pod.Spec.Units, pod.Spec.InitUnits...)
	for _, unit := range units {
		for _, ev := range unit.Env {
			if ev.ValueFrom != nil && ev.ValueFrom.SecretKeyRef != nil {
				name := ev.ValueFrom.SecretKeyRef.Name
				_, nameExists := secrets[name]
				if !nameExists {
					secrets[name] = make(map[string][]byte)
				}
				key := ev.ValueFrom.SecretKeyRef.Key
				s, err := c.secretLister.GetSecret(name)
				if err != nil {
					return nil, util.WrapError(err, "Could not get secret %s from registry", name)
				}
				value, exists := s.Data[key]
				if !exists {
					return nil, fmt.Errorf(
						"Could not find secret key %s in secret %s", key, name)
				}
				secrets[name][key] = value
			}
		}
	}
	return secrets, nil
}

func (c *PodController) updatePodUnits(pod *api.Pod) error {
	node, err := c.nodeLister.GetNode(pod.Status.BoundNodeName)
	if err != nil {
		return util.WrapError(err, "Error getting node %s for pod update", pod.Status.BoundNodeName)
	}
	client := c.nodeClientFactory.GetClient(node.Status.Addresses)
	podSecrets, err := c.loadPodSecrets(pod)
	if err != nil {
		return util.WrapError(err, "Error getting pod %s secrets for pod update", pod.Name)
	}
	podCreds, err := c.loadRegistryCredentials(pod)
	if err != nil {
		return util.WrapError(err, "Unable to sync pod: %s bad Pod.Spec.ImagePullSecret", pod.Name)
	}
	podParams := api.PodParameters{
		Secrets:     podSecrets,
		Credentials: podCreds,
		Spec:        pod.Spec,
		PodName:     pod.Name,
		NodeName:    c.kubernetesNodeName,
		PodIP:       api.GetPodIP(node.Status.Addresses),
	}
	return client.UpdateUnits(podParams)
}

func isBurstableMachine(machine string) bool {
	machineType := strings.ToLower(machine)
	return (strings.HasPrefix(machineType, "t2") ||
		strings.HasPrefix(machineType, "t3"))
}

func (c *PodController) dispatchPodToNode(pod *api.Pod, node *api.Node) {
	glog.Infof("Dispatching pod %s to node %s", pod.Name, node.Name)
	client := c.nodeClientFactory.GetClient(node.Status.Addresses)
	resizableVolume := !c.cloudClient.GetAttributes().FixedSizeVolume
	if resizableVolume && pod.Spec.Resources.VolumeSize != "" {
		err := c.resizeVolume(node, pod, client)
		if err != nil {
			msg := fmt.Sprintf("Error resizing volume on node %s pod %s: %v",
				node.Name, pod.Name, err)
			glog.Errorf("%s", msg)
			c.markFailedPod(pod, true, msg)
			return
		}
	}

	if pod.Spec.Resources.SustainedCPU != nil &&
		isBurstableMachine(node.Spec.InstanceType) {
		err := c.cloudClient.SetSustainedCPU(node, *pod.Spec.Resources.SustainedCPU)
		if err != nil {
			msg := fmt.Sprintf("Error dispatching pod to node, could not modify Sustained CPU settings: %s", err)
			glog.Errorln(msg)
			c.markFailedPod(pod, true, msg)
			return
		}
	}

	// Add labels to the instance but don't fail if that fails, just
	// warn to the user and continue...  Also, lets just launch this
	/// as a goroutine cause we don't care when it finishes
	go c.TagNodeWithPodLabels(pod, node)

	err := c.updatePodUnits(pod)
	if err != nil {
		msg := fmt.Sprintf("Error updating pod units after dispatching pod to node: %v", err)
		glog.Errorln(msg)
		c.markFailedPod(pod, true, msg)
		return
	}

	err = setPodRunning(pod, node.Name, c.podRegistry, c.events)
	if err != nil {
		msg := fmt.Sprintf("Error updating pod status to running: %v", err)
		glog.Error(msg)
		c.markFailedPod(pod, true, msg)
		return
	}
}

func (c *PodController) SyncRunningPods() {
	podList, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		return p.Spec.Phase == api.PodRunning &&
			p.Status.Phase == api.PodRunning
	})
	if err != nil {
		glog.Errorf("Could not list running pods for full sync")
		return
	}
	for _, pod := range podList.Items {
		go func(p *api.Pod) {
			err := c.updatePodUnits(p)
			if err != nil {
				glog.Error(err)
			}
		}(pod)
	}
}

func (c *PodController) TagNodeWithPodLabels(pod *api.Pod, node *api.Node) {
	cloudLabels := util.FilterKeysWithPrefix(pod.Labels, util.InternalLabelPrefixes)
	podName := util.GetNameFromString(pod.Name)
	podNamespace := util.GetNamespaceFromString(pod.Name)
	cloudLabels[cloud.PodNameTagKey] = util.CreateBoundNodeNameTag(c.nametag, podName)
	if podNamespace != "" {
		cloudLabels[cloud.NamespaceTagKey] = podNamespace
	}
	err := c.cloudClient.AddInstanceTags(node.Status.InstanceID, cloudLabels)
	if err != nil {
		glog.Errorln("Error tagging node", node.Name, err)
	}
}

func (c *PodController) failDispatchingPods() {
	podList, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		return p.Status.Phase == api.PodDispatching
	})
	if err != nil {
		glog.Errorf("Could not list dispatching pods")
		return
	}
	for _, pod := range podList.Items {
		go c.nodeDispenser.ReturnNode(pod.Status.BoundNodeName, false)
		pod.Status.Phase = api.PodFailed
		_, err = c.podRegistry.UpdatePodStatus(pod, "Milpa resets/fails dispatching pods at system startup")
		if err != nil {
			glog.Errorf("Error updating pod status: %v", err)
			continue
		}
	}
}

func (c *PodController) handlePodStatusReply(reply FullPodStatus) {
	pod, err := c.podRegistry.GetPod(reply.Name)
	if err != nil {
		glog.Errorf("Error getting pod %s from registry: %v", reply.Name, err)
		return
	}
	podIP := api.GetPrivateIP(pod.Status.Addresses)
	if podIP == "" {
		pod.Status.Addresses = api.NewNetworkAddresses(reply.PodIP, "")
	} else if reply.PodIP != "" && podIP != reply.PodIP {
		// Reply came in after pod has been rescheduled.
		glog.Errorf("IP for pod %s has changed %s -> %s",
			reply.Name, reply.PodIP, podIP)
		return
	}

	changed, startFailure, failMsg := updatePodWithStatus(pod, reply)
	if changed {
		if failMsg != "" {
			c.markFailedPod(pod, startFailure, failMsg)
			return
		}
		_, err = c.podRegistry.UpdatePodStatus(pod, "Updating pod unit statuses")
		if err != nil {
			// The update will fail if we have termianted the pod so don't
			// spew false errors to the logs if that's the case.  Get the pod
			// and check the Status.Phase
			savedPod, _ := c.podRegistry.GetPod(pod.Name)
			if savedPod == nil || !api.IsTerminalPodPhase(savedPod.Status.Phase) {
				glog.Errorf("Error updating pod %s status: %v", pod.Name, err)
			}
		}
	}

	if len(reply.ResourceUsage) > 0 {
		c.metricsRegistry.Insert(pod.Name, api.Now(), reply.ResourceUsage)
	}
}

// Remove pods from the map that have been terminated.
func (c *PodController) pruneLastStatusReplies() {
	runningPods := make(map[string]bool)
	_, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		if p.Status.Phase == api.PodRunning {
			runningPods[p.Name] = true
		}
		return false
	})
	if err != nil {
		glog.Errorf("Error getting list of pods from registry")
		return
	}
	for _, replyItem := range c.lastStatusReply.Items() {
		replyPodName := replyItem.Key
		_, exists := runningPods[replyPodName]
		if !exists {
			c.lastStatusReply.Delete(replyPodName)
		}
	}
}

// Handle pods that failed to respond to status requests.
func (c *PodController) handleReplyTimeouts() {
	podList, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		return p.Status.Phase == api.PodRunning
	})
	if err != nil {
		glog.Errorf("Error getting list of pods from registry")
		return
	}
	now := time.Now().UTC()
	for _, pod := range podList.Items {
		last, exists := c.lastStatusReply.GetOK(pod.Name)
		if !exists {
			c.lastStatusReply.Set(pod.Name, now)
			continue
		}
		if now.Sub(last) < statusReplyTimeout {
			continue
		}
		go c.maybeFailUnresponsivePod(pod)
	}
}

func (c *PodController) maybeFailUnresponsivePod(pod *api.Pod) {
	node, err := c.nodeLister.GetNode(pod.Status.BoundNodeName)
	if err != nil {
		msg := fmt.Sprintf("No node found for pod %s", pod.Name)
		glog.Warningf(msg)
		c.markFailedPod(pod, false, msg)
		return
	}
	client := c.nodeClientFactory.GetClient(node.Status.Addresses)
	_, err = client.GetStatus()
	if err != nil {
		msg := fmt.Sprintf("No status reply from pod %s in %ds failing pod",
			pod.Name, int(statusReplyTimeout.Seconds()))
		glog.Warningf(msg)
		c.markFailedPod(pod, false, msg)
	} else {
		glog.Warningf("Last chance healthcheck for pod %s saved the pod from failure. Pod status is possibly out of date", pod.Name)
		c.lastStatusReply.Set(pod.Name, time.Now().UTC())
	}
}

// Periodically we should go through and do a consistency check of the
// nodes we have claimed.  We look to see if we are really using them
// claimed but unused nodes can come from a few places, most likely a
// race between shutting down the server and dispatching.  Also,
// programming errors.  It will be interesting to see if either of
// these ever come up in reality.
func (c *PodController) checkClaimedNodes() {
	// create set of pods -> running nodes
	// list nodes through api, only care about claimed, map of nodeName -> podName

	// go through list of claimed nodes, see if they are running the correct pod
	// those that aren't are moved into the wrong pod list
	// those that are in both wrongPod and lastWrongPod are
	// returned through the node dispenser

	// maps from node name to pod name
	wrongPod := make(map[string]string)
	podList, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		return p.Status.Phase == api.PodDispatching ||
			p.Status.Phase == api.PodRunning
	})
	if err != nil {
		glog.Error(err)
		return
	}
	podToNode := make(map[string]string)
	for _, pod := range podList.Items {
		podToNode[pod.Name] = pod.Status.BoundNodeName
	}

	nodeList, err := c.nodeLister.ListNodes(registry.MatchAllNodes)
	if err != nil {
		glog.Error(err)
		return
	}
	for _, node := range nodeList.Items {
		if node.Status.Phase == api.NodeClaimed {
			bp := node.Status.BoundPodName
			bn, exists := podToNode[bp]
			if !exists || bn != node.Name {
				wrongPod[node.Name] = bp
			}
		}
	}

	for nodeName, podName := range lastWrongPod {
		lastPodName, exists := wrongPod[nodeName]
		if exists && lastPodName == podName {
			glog.Errorf("Found claimed node %s with incorrect pod assignment %s",
				nodeName, podName)
			c.nodeDispenser.ReturnNode(nodeName, false)
		}
	}
	lastWrongPod = wrongPod
}

// make sure that all running pods are
// actually running on the nodes they say they're running on
func (c *PodController) checkRunningPods() {
	// get claimed nodes, store in nodeName -> podName
	// go through running pods, get BoundNodeName, compare to nodeToPod
	// if they're different, add to wrongNode

	// maps from pod name to node name
	wrongNode := make(map[string]string)

	nodeList, err := c.nodeLister.ListNodes(registry.MatchAllNodes)
	if err != nil {
		glog.Error(err)
		return
	}
	nodeToPod := make(map[string]string)
	for _, node := range nodeList.Items {
		if node.Status.Phase == api.NodeClaimed {
			nodeToPod[node.Name] = node.Status.BoundPodName
		}
	}
	podList, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		return p.Status.Phase == api.PodRunning
	})
	if err != nil {
		glog.Error(err)
		return
	}

	for _, pod := range podList.Items {
		boundNodeName := nodeToPod[pod.Status.BoundNodeName]
		if boundNodeName == "" || boundNodeName != pod.Name {
			wrongNode[pod.Name] = pod.Status.BoundNodeName
		}
	}
	for podName, nodeName := range lastWrongNode {
		lastNodeName, exists := wrongNode[podName]
		if exists && lastNodeName == nodeName {
			msg := fmt.Sprintf("Found running pod %s with incorrect node assignment %s",
				podName, nodeName)
			glog.Errorf("%s", msg)
			pod, err := c.podRegistry.GetPod(podName)
			if err != nil {
				glog.Errorf("Getting broken pod from registry: %v", err)
				continue
			}
			pod.Status.Phase = api.PodFailed
			_, err = c.podRegistry.UpdatePodStatus(pod, msg)
			if err != nil {
				glog.Errorf("Error updating pod status: %v", err)
				continue
			}
		}
	}
	lastWrongNode = wrongNode
}

// Here we're mostly copying parameters from the node to the pod
// and updating the pod status
func (c *PodController) setPodDispatchingParams(pod *api.Pod, node *api.Node) (*api.Pod, error) {
	pod.Status.BoundNodeName = node.Name
	pod.Status.BoundInstanceID = node.Status.InstanceID
	// The dispatching state is used to keep track of pods
	// that are creating but have received a node from the
	// node manager.  Also, if the management console
	// restarts in the middle of dispatching, we want to return
	// the node to the node manager (as dirty so it gets cleaned
	// since we don't know at what point this dispatch was
	// stopped) and then mark the pod as failed so it gets
	// re-dispatched.
	pod.Status.Phase = api.PodDispatching
	// There's no race here between 2 goroutines trying to dispatch
	// the same pod, only one goroutine can set the pod as
	// dispatching, if we fail, the node is still clean so we tell the
	// node_controller it can be reused.
	msg := fmt.Sprintf("scheduling pod %s onto %s", pod.Name, node.Name)
	_, err := c.podRegistry.UpdatePodStatus(pod, msg)
	return pod, err
}

func (c *PodController) schedulePod(pod *api.Pod) {
	// Get a free node from the nodeDispenser
	// which gets nodes from the node_controller. The
	// request has the pod name so that the node_controller
	// can keep track of who has requested its nodes
	nodeReply := c.nodeDispenser.RequestNode(*pod)
	if nodeReply.Node == nil {
		return
	}
	pod, err := c.setPodDispatchingParams(pod, nodeReply.Node)
	if err != nil {
		glog.Errorf("Error updating pod for dispatch to node: %s", err)
		c.nodeDispenser.ReturnNode(nodeReply.Node.Name, true)
		return
	}
	go c.dispatchPodToNode(pod, nodeReply.Node)
}

func (c *PodController) terminateUnboundPod(pod *api.Pod) {
	c.podRegistry.TerminatePod(pod, api.PodTerminated, "Terminating unbound pod")
}

func (c *PodController) terminateBoundPod(pod *api.Pod) {
	c.podRegistry.TerminatePod(pod, api.PodTerminated, "Terminating bound pod")

	// run this in a goroutine in case it blocks (shouldn't ever happen)
	go func() {
		c.savePodLogs(pod)
		glog.Infof("Returning node %s for pod %s", pod.Status.BoundNodeName, pod.Name)
		c.nodeDispenser.ReturnNode(pod.Status.BoundNodeName, false)
	}()
}

func (c *PodController) queryPodStatus(pod *api.Pod) FullPodStatus {
	node, err := c.nodeLister.GetNode(pod.Status.BoundNodeName)
	if err != nil {
		reply := FullPodStatus{
			Name:             pod.Name,
			PodIP:            "",
			UnitStatuses:     nil,
			InitUnitStatuses: nil,
			Error:            err,
		}
		return reply
	}
	client := c.nodeClientFactory.GetClient(node.Status.Addresses)
	replyStatuses, err := client.GetStatus()
	if err != nil {
		reply := FullPodStatus{
			Name:             pod.Name,
			PodIP:            "",
			UnitStatuses:     nil,
			InitUnitStatuses: nil,
			Error:            err,
		}
		return reply
	}
	c.lastStatusReply.Set(pod.Name, time.Now().UTC())
	replyMap := make(map[string]api.UnitStatus)
	for _, s := range replyStatuses.UnitStatuses {
		replyMap[s.Name] = s
	}
	for _, s := range replyStatuses.InitUnitStatuses {
		replyMap[s.Name] = s
	}
	reply := FullPodStatus{
		Name:             pod.Name,
		PodIP:            replyStatuses.PodIP,
		UnitStatuses:     filterUnitStatuses(pod.Spec.Units, replyMap),
		InitUnitStatuses: filterUnitStatuses(pod.Spec.InitUnits, replyMap),
		ResourceUsage:    replyStatuses.ResourceUsage,
		Error:            nil,
	}
	return reply
}

func filterUnitStatuses(units []api.Unit, statusmap map[string]api.UnitStatus) []api.UnitStatus {
	// Not sure if we should do this but I'm going to filter out
	// statuses for units that aren't in our spec and add un-acked
	// units with status=waiting.  That happens when the node is busy
	// updating (pulling and running) unit updates, usually right
	// after dispatching the pod to the node.
	statuses := make([]api.UnitStatus, 0, len(units))
	for _, u := range units {
		s, exists := statusmap[u.Name]
		if !exists {
			waitingStatus := api.UnitStatus{
				Name: u.Name,
				State: api.UnitState{
					Waiting: &api.UnitStateWaiting{
						Reason: "Unit unacknowledged by node",
					},
				},
				Image: u.Image,
			}
			statuses = append(statuses, waitingStatus)
		} else {
			statuses = append(statuses, s)
		}
	}
	return statuses
}

func (c *PodController) checkRunningPodStatus() {
	podList, err := c.podRegistry.ListPods(func(p *api.Pod) bool {
		return p.Status.Phase == api.PodRunning
	})
	if err != nil {
		glog.Errorln("Error listing running pods", err)
		return
	}
	for _, pod := range podList.Items {
		go func(p *api.Pod) {
			reply := c.queryPodStatus(p)
			if reply.Error != nil {
				glog.Errorf("Error getting status of pod %s: %v",
					reply.Name, reply.Error)
			} else {
				c.handlePodStatusReply(reply)
			}
		}(pod)
	}
}

// This should be run from a goroutine to keep from blocking the
// main control loop.  As such, we'll pass in the pod addresses since
// items in pod.Status can change behind the scenes.
func (c *PodController) savePodLogs(pod *api.Pod) {
	if pod.Status.BoundNodeName == "" {
		glog.Infof("not saving pod logs, pod is not bound")
		return
	}

	node, err := c.nodeLister.GetNode(pod.Status.BoundNodeName)
	if err != nil {
		glog.Infof("not saving pod logs, bound to node %q: %v",
			pod.Status.BoundNodeName, err)
		return
	}

	glog.Infof("Saving pod logs")
	podAddresses := node.Status.Addresses

	if len(podAddresses) == 0 {
		glog.Infof("pod %s has no bound instance, not gathering logs",
			pod.Name)
	}
	client := c.nodeClientFactory.GetClient(podAddresses)
	podRef := api.ToObjectReference(pod)
	allUnits := api.AllPodUnits(pod)
	for _, unit := range allUnits {
		data, err := client.GetLogs(unit.Name, 0, nodeclient.SAVE_LOG_BYTES)
		if err != nil {
			glog.Errorf("Error saving pod %s log for unit %s: %s",
				pod.Name, unit.Name, err.Error())
			continue
		}
		log := api.NewLogFile()
		log.Name = unit.Name
		log.ParentObject = podRef
		log.Content = string(data)
		_, err = c.logRegistry.CreateLog(log)
		if err != nil {
			glog.Errorf("Error saving pod %s log for unit %s to registry: %s",
				pod.Name, unit.Name, err.Error())
		}
	}
}

func (c *PodController) handlePodSucceeded(pod *api.Pod) {
	glog.Errorf("Pod %s has succeeded", pod.Name)
	_, err := c.podRegistry.TerminatePod(pod, api.PodSucceeded, "Pod succeeded")
	if err != nil {
		glog.Errorf("Error updating pod %s spec phase: %v",
			pod.Name, err)
	}
	// Pod's work is done...
	go func() {
		c.savePodLogs(pod)
		c.nodeDispenser.ReturnNode(pod.Status.BoundNodeName, false)
	}()
	//c.deleteFinishedPod(pod)
}

func podNeedsControlling(p *api.Pod) bool {
	return p.Spec.Phase != p.Status.Phase
}

// We do all pod controlling in one loop since there are windows for
// races otherwise.
func (c *PodController) ControlPods() {
	podlist, err := c.podRegistry.ListPods(podNeedsControlling)
	if err != nil {
		glog.Errorf("Error listing pods %v", err)
	}
	if len(podlist.Items) <= 0 {
		return
	}
	for _, pod := range podlist.Items {
		switch pod.Spec.Phase {
		case api.PodRunning:
			// if creating, try to dispatch it
			// if dispatching, log that it hasn't started yet
			// if running, log that we shouldn't see this
			// if failed, see if we should terminate it with an err
			// if terminated, we don't care
			switch pod.Status.Phase {
			case api.PodWaiting:
				c.schedulePod(pod)
			case api.PodDispatching:
				glog.Warningf("Previously dispatching pod %s is not finished dispatching", pod.Name)
			case api.PodRunning:
				glog.Warningf("Pod %s is already in desired state, no control necessary", pod.Name)
			case api.PodFailed:
				remedyFailedPod(pod, c.podRegistry)
			case api.PodSucceeded:
				c.handlePodSucceeded(pod)
			case api.PodTerminated:
				// We've likely set this pod as dead after too many failures
				glog.Warningf("pod %s is terminated but speced to be running", pod.Name)
			default:
				glog.Errorf("Unknown pod phase: %s", pod.Status.Phase)
			}
		case api.PodTerminated:
			// if waiting, just mark it as terminated
			// if dispatching, log that we will try to terminate it soon
			// -- todo: keep track of how long a pod is stuck in dispatching
			//    and eventually time it out and take the necessary steps to
			//    reclaim resources
			//    Probably need pod conditions for tracking this
			// if running, stop the app and move status to terminated
			// if failed or succeeded, move to Terminated
			//
			switch pod.Status.Phase {
			case api.PodWaiting, api.PodFailed, api.PodSucceeded:
				c.terminateUnboundPod(pod)
			case api.PodDispatching, api.PodRunning:
				c.terminateBoundPod(pod)
			}
		}
	}
}
