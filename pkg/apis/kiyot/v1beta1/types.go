package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// A couple of notes on how to use all of this
// 1. This relies on k8s.io/code-generator that should be vendored into the repo
// 2. Execute scripts/update-codegen.sh (pulled from https://github.com/programming-kubernetes/cnat)
// 3. That script will update/create pkg/k8sclient, everything in there is autogenerated.  If things go south, delete that folder and it'll get recreated
// 4. See if everything is OK with scripts/verify-codegen.sh

var (
	KiyotCRDDefString = `
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  # name must match the spec fields below, and be in the form: <plural>.<group>
  name: cells.kiyot.elotl.co
spec:
  # group name to use for REST API: /apis/<group>/<version>
  group: kiyot.elotl.co
  # list of versions supported by this CustomResourceDefinition
  versions:
    - name: v1beta1
      # Each version can be enabled/disabled by Served flag.
      served: true
      # One and only one version must be marked as the storage version.
      storage: true
  # either Namespaced or Cluster
  scope: Cluster
  names:
    # plural name to be used in the URL: /apis/<group>/<version>/<plural>
    plural: cells
    # singular name to be used as an alias on the CLI and for display
    singular: cell
    # kind is normally the CamelCased singular type. Your resource manifests use this.
    kind: Cell
    # shortNames allow shorter string to match your resource on the CLI
    shortNames:
    - cl
  preserveUnknownFields: true
  validation:
    openAPIV3Schema:
      type: object
      properties:
        status:
          type: object
          properties:
            podName:
              type: string
            kubelet:
              type: string
            controllerID:
              type: string
            launchType:
              type: string
            instanceType:
              type: string
            ip:
              type: string
  additionalPrinterColumns:
    - name: Pod Name
      type: string
      description: The name of the pod
      JSONPath: .status.podName
    - name: Kubelet
      type: string
      description: The name of the kubelet that launched the pod
      JSONPath: .status.kubelet
    - name: Launch Type
      type: string
      description: The underlying cloud compute technology running the pod
      JSONPath: .status.launchType
    - name: Instance Type
      type: string
      description: Name of cloud instance type running the pod
      JSONPath: .status.instanceType
    - name: Instance ID
      type: string
      description: The cloud proider's ID of the instance running the pod
      JSONPath: .status.instanceID
    - name: IP
      type: string
      description: The IP address of the node
      JSONPath: .status.ip
`
)

// Note: as of 7/30/19 you need a blank line between the genclient
// and other comments and the struct definition. That should get
// fixed upstream eventually...

// +genclient
// +genclient:nonNamespaced
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Cell struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Status CellStatus `json:"status,omitempty"`
}

type CellStatus struct {
	PodName string `json:"podName,omitempty"`
	// Todo: this should probably be NodeName
	Kubelet      string `json:"kubelet,omitempty"`
	ControllerID string `json:"controllerID,omitempty"`
	LaunchType   string `json:"launchType,omitempty"`
	InstanceType string `json:"instanceType,omitempty"`
	InstanceID   string `json:"instanceID,omitempty"`
	IP           string `json:"ip,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CellList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []Cell `json:"items"`
}
