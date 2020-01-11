package validation

// Copyright 2014 The Kubernetes Authors.
// Copyright 2017 Elotl Inc.

// Taken from k8s at commit: 2296108886a29db5cb7be73412eb562cdbf1cb74
// That commit was chosen for no real particular reason other than it
// didn't use a bunch of apimachinery and other junk and imports to
// support validation were relatively light.
//
// Some pieces of K8s code were copy-pasted into here to prevent
// having to bring in abstractions/organization that we don't use

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/elotl/cloud-instance-provider/pkg/api"
	"github.com/elotl/cloud-instance-provider/pkg/util"
	"github.com/elotl/cloud-instance-provider/pkg/util/instanceselector"
	"github.com/elotl/cloud-instance-provider/pkg/util/sets"
	"github.com/elotl/cloud-instance-provider/pkg/util/validation"
	"github.com/elotl/cloud-instance-provider/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/api/resource"
)

const isNegativeErrorMsg string = `must be greater than or equal to 0`
const isNotPositiveErrorMsg string = `must be greater than or equal to 1`
const invalidPathMsg string = "path must exist"
const totalAnnotationSizeLimitB int = 256 * (1 << 10) // 256 kB
const RestAPIPort = 6421
const MaxSecretSize = 1 * 1024 * 1024

// ValidateNameFunc validates that the provided name is valid for a
// given resource type.  Not all resources have the same validation
// rules for names. Prefix is true if the name will have a value
// appended to it.  If the name is not valid, this returns a list of
// descriptions of individual characteristics of the value that were
// not valid.  Otherwise this returns an empty list or nil.
type ValidateNameFunc func(name string, prefix bool) []string

// maskTrailingDash replaces the final character of a string with a subdomain safe
// value if is a dash.
func maskTrailingDash(name string) string {
	if strings.HasSuffix(name, "-") {
		return name[:len(name)-2] + "a"
	}
	return name
}

// ValidatePodName can be used to check whether the given pod name is
// valid.  Prefix indicates this name will be used as part of
// generation, in which case trailing dashes are allowed.
var ValidatePodName = NameIsValidPodName
var ValidateNodeName = NameIsDNSSubdomain
var ValidateServiceName = NameIsDNS952Label
var ValidateSecretName = NameIsDNSSubdomain

// Pod names are unique, they get to have slashes in them
func NameIsValidPodName(name string, prefix bool) []string {
	return validation.IsValidPodName(name)
}

// NameIsDNSSubdomain is a ValidateNameFunc for names that must be a DNS subdomain.
func NameIsDNSSubdomain(name string, prefix bool) []string {
	if prefix {
		name = maskTrailingDash(name)
	}
	return validation.IsDNS1123Subdomain(name)
}

// NameIsDNSLabel is a ValidateNameFunc for names that must be a DNS 1123 label.
func NameIsDNSLabel(name string, prefix bool) []string {
	if prefix {
		name = maskTrailingDash(name)
	}
	return validation.IsDNS1123Label(name)
}

// NameIsDNS952Label is a ValidateNameFunc for names that must be a DNS 952 label.
func NameIsDNS952Label(name string, prefix bool) []string {
	if prefix {
		name = maskTrailingDash(name)
	}
	return validation.IsDNS952Label(name)
}

func ValidateDNS1123Label(value string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, msg := range validation.IsDNS1123Label(value) {
		allErrs = append(allErrs, field.Invalid(fldPath, value, msg))
	}
	return allErrs
}

func ValidateDNS1123Subdomain(value string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, msg := range validation.IsDNS1123Subdomain(value) {
		allErrs = append(allErrs, field.Invalid(fldPath, value, msg))
	}
	return allErrs
}

// Validates that given value is not negative.
func ValidateNonnegativeField(value int64, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if value < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, value, isNegativeErrorMsg))
	}
	return allErrs
}

// Validates that given value is not negative.
func ValidatePositiveField(value int, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if value <= 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, value, isNotPositiveErrorMsg))
	}
	return allErrs
}

func ValidateFileExists(path string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			allErrs = append(allErrs, field.Invalid(fldPath, path, invalidPathMsg))
		} else {
			msg := fmt.Sprintf("Error accessing file: %v", err)
			allErrs = append(allErrs, field.Invalid(fldPath, path, msg))
		}
	} else if info.IsDir() {
		allErrs = append(allErrs, field.Invalid(fldPath, path, "path must point to a file, not a directory"))
	}
	return allErrs
}

// ValidateAnnotations validates that a set of annotations are correctly defined.
func ValidateAnnotations(annotations map[string]string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	var totalSize int64
	for k, v := range annotations {
		for _, msg := range validation.IsQualifiedName(strings.ToLower(k)) {
			allErrs = append(allErrs, field.Invalid(fldPath, k, msg))
		}
		totalSize += (int64)(len(k)) + (int64)(len(v))
	}
	if totalSize > (int64)(totalAnnotationSizeLimitB) {
		allErrs = append(allErrs, field.TooLong(fldPath, "", totalAnnotationSizeLimitB))
	}
	return allErrs
}

// ValidateLabelName validates that the label name is correctly defined.
func ValidateLabelName(labelName string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, msg := range validation.IsQualifiedName(labelName) {
		allErrs = append(allErrs, field.Invalid(fldPath, labelName, msg))
	}
	return allErrs
}

// ValidateLabels validates that a set of labels are correctly defined.
func ValidateLabels(labels map[string]string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for k, v := range labels {
		allErrs = append(allErrs, ValidateLabelName(k, fldPath)...)
		for _, msg := range validation.IsValidLabelValue(v) {
			allErrs = append(allErrs, field.Invalid(fldPath, v, msg))
		}
	}
	return allErrs
}

// ValidateObjectMeta validates an object's metadata on creation. It
// expects that name generation has already been performed.  It
// doesn't return an error for rootscoped resources with namespace,
// because namespace should already be cleared before.  TODO: Remove
// calls to this method scattered in validations of specific
// resources, e.g., ValidatePodUpdate.
func ValidateObjectMeta(meta *api.ObjectMeta, requiresNamespace bool, nameFn ValidateNameFunc, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(meta.Name) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), "name or generateName is required"))
	} else {
		for _, msg := range nameFn(meta.Name, false) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), meta.Name, msg))
		}
	}
	allErrs = append(allErrs, ValidateLabels(meta.Labels, fldPath.Child("labels"))...)
	return allErrs
}

func validateEnv(vars []api.EnvVar, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	for i, ev := range vars {
		idxPath := fldPath.Index(i)
		if len(ev.Name) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("name"), ""))
		} else {
			for _, msg := range validation.IsCIdentifier(ev.Name) {
				allErrs = append(allErrs, field.Invalid(idxPath.Child("name"), ev.Name, msg))
			}
		}
		allErrs = append(allErrs, validateEnvVarValueFrom(ev, idxPath.Child("valueFrom"))...)
	}
	return allErrs
}

func validateEnvVarValueFrom(ev api.EnvVar, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if ev.ValueFrom == nil {
		return allErrs
	}
	if ev.ValueFrom.SecretKeyRef != nil {
		allErrs = append(allErrs, validateSecretKeySelector(ev.ValueFrom.SecretKeyRef, fldPath.Child("secretKeyRef"))...)
	} else {
		allErrs = append(allErrs, field.Invalid(fldPath, "", "must specify one of: `secretKeyRef`"))
	}
	return allErrs
}

func validateSecretKeySelector(s *api.SecretKeySelector, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	nameFn := ValidateNameFunc(ValidateSecretName)
	for _, msg := range nameFn(s.Name, false) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), s.Name, msg))
	}
	if len(s.Key) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("key"), ""))
	} else {
		for _, msg := range validation.IsConfigMapKey(s.Key) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("key"), s.Key, msg))
		}
	}

	return allErrs
}

func validateRestartPolicy(restartPolicy *api.RestartPolicy, fldPath *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}
	switch *restartPolicy {
	case api.RestartPolicyAlways, api.RestartPolicyOnFailure, api.RestartPolicyNever:
		break
	case "":
		allErrors = append(allErrors, field.Required(fldPath, ""))
	default:
		validValues := []string{string(api.RestartPolicyAlways), string(api.RestartPolicyOnFailure), string(api.RestartPolicyNever)}
		allErrors = append(allErrors, field.NotSupported(fldPath, *restartPolicy, validValues))
	}

	return allErrors
}

func validateSpotPolicy(spotPolicy *api.SpotPolicy, fldPath *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}
	switch *spotPolicy {
	case api.SpotAlways, api.SpotNever:
		break
	case "":
		allErrors = append(allErrors, field.Required(fldPath, ""))
	default:
		validValues := []string{string(api.SpotAlways), string(api.SpotNever)}
		allErrors = append(allErrors, field.NotSupported(fldPath, *spotPolicy, validValues))
	}

	return allErrors
}

// ValidatePod tests if required fields in the pod are set.
func ValidatePod(pod *api.Pod) field.ErrorList {
	fldPath := field.NewPath("metadata")
	allErrs := ValidateObjectMeta(&pod.ObjectMeta, true, ValidatePodName, fldPath)
	allErrs = append(allErrs, ValidatePodSpec(&pod.Spec, field.NewPath("spec"))...)
	return allErrs
}

func ValidateResourceParses(resourceStr string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if resourceStr != "" {
		if _, err := resource.ParseQuantity(resourceStr); err != nil {
			msg := fmt.Sprintf("Invalid quantity format specified: %v", err)
			allErrs = append(allErrs, field.Invalid(fldPath, resourceStr, msg))
		}
	}
	return allErrs
}

func validateResourceSpec(rs *api.ResourceSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateResourceParses(rs.Memory, fldPath.Child("Memory"))...)
	allErrs = append(allErrs, ValidateResourceParses(rs.CPU, fldPath.Child("CPU"))...)
	allErrs = append(allErrs, ValidateResourceParses(rs.GPU, fldPath.Child("GPU"))...)
	allErrs = append(allErrs, ValidateResourceParses(rs.VolumeSize, fldPath.Child("VolumeSize"))...)

	return allErrs
}

func ValidateInstanceType(instanceType string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if instanceType == "" {
		return allErrs
	}
	if instanceselector.IsUnsupportedInstance(instanceType) {
		msg := "The instance type you have selected is not supported by Milpa at this time"
		allErrs = append(allErrs, field.Invalid(fldPath, instanceType, msg))
	}
	return allErrs
}

// ValidatePodSpec tests that the specified PodSpec has valid data.
// This includes checking formatting and uniqueness.  It also canonicalizes the
// structure by setting default values and implementing any backwards-compatibility
// tricks.
func ValidatePodSpec(spec *api.PodSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allVolumes, vErrs := validateVolumes(spec.Volumes, fldPath.Child("volumes"))
	allErrs = append(allErrs, vErrs...)

	allErrs = append(allErrs, validateResourceSpec(&spec.Resources, fldPath.Child("Resources"))...)
	allErrs = append(allErrs, validateRestartPolicy(&spec.RestartPolicy, fldPath.Child("restartPolicy"))...)
	allErrs = append(allErrs, validateUnits(spec.Units, allVolumes, fldPath.Child("units"))...)
	allErrs = append(allErrs, validateInitUnits(spec.InitUnits, spec.Units, allVolumes, fldPath.Child("initUnits"))...)
	allErrs = append(allErrs, validatePodSpot(spec.Spot, fldPath.Child("spot"))...)
	allErrs = append(allErrs, ValidateInstanceType(spec.InstanceType, fldPath.Child("instanceType"))...)
	allErrs = append(allErrs, validatePodSecurityContext(spec.SecurityContext, fldPath.Child("SecurityContext"))...)
	return allErrs
}

func validatePodSecurityContext(context *api.PodSecurityContext, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if context == nil {
		return allErrs
	}
	for i, sysctl := range context.Sysctls {
		idxPath := fldPath.Index(i)
		if sysctl.Name == "" || sysctl.Value == "" {
			msg := fmt.Sprintf("Invalid sysctl name='%s' value='%s'",
				sysctl.Name, sysctl.Value)
			allErrs = append(allErrs, field.Invalid(idxPath, sysctl, msg))
		}
	}
	return allErrs
}

func validateUnits(units []api.Unit, volumes sets.String, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// in milpa, you can have 0 units and it's all good
	allNames := sets.String{}
	allPorts := []api.ServicePort{}
	for i, unit := range units {
		idxPath := fldPath.Index(i)
		namePath := idxPath.Child("name")
		if len(unit.Name) == 0 {
			allErrs = append(allErrs, field.Required(namePath, ""))
		} else {
			allErrs = append(allErrs, ValidateDNS1123Label(unit.Name, namePath)...)
		}
		if allNames.Has(unit.Name) {
			allErrs = append(allErrs, field.Duplicate(namePath, unit.Name))
		} else {
			allNames.Insert(unit.Name)
		}
		if len(unit.Image) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("image"), ""))
		}
		_, _, err := util.ParseImageSpec(unit.Image)
		if err != nil {
			msg := "Invalid image format: must be one of ACCOUNT.dkr.ecr.REGION.amazonaws.com/reponame, url/namespace/reponame, namespace/reponame or reponame"
			allErrs = append(allErrs, field.Invalid(idxPath.Child("image"), unit.Image, msg))
		}
		portsPath := idxPath.Child("ports")
		for i, port := range unit.Ports {
			portPath := portsPath.Index(i)
			allErrs = append(allErrs, ValidateServicePort(&port, false, portPath)...)
			allPorts = append(allPorts, port)
		}
		allErrs = append(allErrs, validateEnv(unit.Env, idxPath.Child("env"))...)
		allErrs = append(allErrs, validateVolumeMounts(unit.VolumeMounts, volumes, idxPath.Child("volumeMounts"))...)
		//
		// todo: validate probes when we get probes
		//
	}
	allErrs = append(allErrs, validateServicePortUniqueness(allPorts, false, fldPath)...)
	allErrs = append(allErrs, validateServicePortOverlap(allPorts, fldPath)...)
	return allErrs
}

func validateInitUnits(units, otherUnits []api.Unit, volumes sets.String, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if len(units) > 0 {
		allErrs = append(allErrs, validateUnits(units, volumes, fldPath)...)
	}
	allNames := sets.String{}
	for _, unit := range otherUnits {
		allNames.Insert(unit.Name)
	}
	for i, unit := range units {
		idxPath := fldPath.Index(i)
		if allNames.Has(unit.Name) {
			allErrs = append(allErrs, field.Duplicate(idxPath.Child("name"), unit.Name))
		}
		if len(unit.Name) > 0 {
			allNames.Insert(unit.Name)
		}
	}
	return allErrs
}

func validatePodSpot(spot api.PodSpot, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, validateSpotPolicy(&spot.Policy, fldPath.Child("policy"))...)
	return allErrs
}

// ValidateNode tests if required fields in the node are set.
func ValidateNode(node *api.Node) field.ErrorList {
	fldPath := field.NewPath("metadata")
	allErrs := ValidateObjectMeta(&node.ObjectMeta, false, ValidateNodeName, fldPath)
	return allErrs
}

// ValidateService tests if required fields in the service are set.
func ValidateService(service *api.Service) field.ErrorList {
	allErrs := ValidateObjectMeta(&service.ObjectMeta, true, ValidateServiceName, field.NewPath("metadata"))

	if service.Name == util.MilpaSvcName {
		msg := fmt.Sprintf("Service names cannot be called %s, that suffix is reserved for internal use by milpa", util.MilpaSvcName)
		allErrs = append(allErrs, field.Invalid(field.NewPath("metadata.name"), service.Name, msg))
	}

	specPath := field.NewPath("spec")
	if len(service.Spec.Ports) == 0 {
		allErrs = append(allErrs, field.Required(specPath.Child("ports"), ""))
	}

	portsPath := specPath.Child("ports")
	for i := range service.Spec.Ports {
		portPath := portsPath.Index(i)
		allErrs = append(allErrs, ValidateServicePort(&service.Spec.Ports[i], len(service.Spec.Ports) > 1, portPath)...)
	}

	allErrs = append(allErrs, ValidateLabelSelector(&service.Spec.Selector, specPath.Child("selector"))...)
	_, err := api.LabelSelectorAsSelector(&service.Spec.Selector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(specPath.Child("selector"), service.Spec.Selector, "invalid label selector in service."))
	}

	portsPath = specPath.Child("ports")
	allErrs = append(allErrs, validateServicePortUniqueness(service.Spec.Ports, true, portsPath)...)
	allErrs = append(allErrs, validateServicePortOverlap(service.Spec.Ports, portsPath)...)

	sourceRangesPath := specPath.Child("sourceRanges")
	sourceRangeSet := sets.NewString()
	for i, sr := range service.Spec.SourceRanges {
		idxPath := sourceRangesPath.Index(i)
		cidrStr := sr
		if sr != "VPC" {
			_, ipnet, err := net.ParseCIDR(sr)
			if err != nil {
				allErrs = append(allErrs, field.Invalid(idxPath, sr, "sourceRange items must be a CIDR network address"))
				continue
			}
			cidrStr = ipnet.String()
		}
		if sourceRangeSet.Has(cidrStr) {
			allErrs = append(allErrs, field.Duplicate(idxPath, sr))
		}
		sourceRangeSet.Insert(cidrStr)
	}

	// Todo: service name + cluster + namespace + 2 periods must be <=
	// 63 chars in GCE.  For node ports the hashed name takes up 26 chars so,
	// name + cluster + periods must fit in 37 chars.  Cluster name and
	// namespace must have a total length of <= 35 chars
	// Load balancers add 2 characters to this, add this validation
	// to our stateful validation code

	return allErrs
}

func validateServicePortUniqueness(ports []api.ServicePort, validateName bool, fldPath *field.Path) field.ErrorList {
	nodePorts := make(map[api.ServicePort]bool)
	allErrs := field.ErrorList{}
	allNames := sets.NewString()
	for i, port := range ports {
		portPath := fldPath.Index(i)
		var key api.ServicePort
		key.Protocol = port.Protocol
		key.Port = port.Port
		_, found := nodePorts[key]
		if found {
			allErrs = append(allErrs, field.Duplicate(portPath, port.Port))
		}
		if validateName && allNames.Has(port.Name) {
			allErrs = append(allErrs, field.Duplicate(portPath, port.Port))
		}
		allNames.Insert(port.Name)
		nodePorts[key] = true
	}
	return allErrs
}

func validateServicePortOverlap(ports []api.ServicePort, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for i := range ports {
		portPath := fldPath.Index(i)
		for j := i + 1; j < len(ports); j++ {
			if i == j {
				continue
			}
			if ports[i].Protocol != ports[j].Protocol {
				continue
			}
			p1Start := ports[i].Port
			p1End := p1Start + ports[i].PortRangeSize - 1
			p2Start := ports[j].Port
			p2End := p2Start + ports[j].PortRangeSize - 1
			if p1Start <= p2End && p2Start <= p1End {
				msg := fmt.Sprintf(
					"Port ranges overlap: Port %d: %d-%d, Port %d: %d-%d",
					i, p1Start, p1End,
					j, p2Start, p2End)
				allErrs = append(allErrs, field.Invalid(portPath, ports[i], msg))
			}
		}
	}
	return allErrs
}

var supportedPortProtocols = sets.NewString(string(api.ProtocolTCP), string(api.ProtocolUDP), string(api.ProtocolICMP))

var supportedLoadBalancerProtocols = sets.NewString(string(api.ProtocolTCP))

// Made this exported since we call it in server.config.go
func ValidateServicePort(sp *api.ServicePort, requireName bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allNames := sets.NewString()
	if requireName && len(sp.Name) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("name"), ""))
	} else if len(sp.Name) != 0 {
		allErrs = append(allErrs, ValidateDNS1123Label(sp.Name, fldPath.Child("name"))...)
		// We'll force names to be unique
		if allNames.Has(sp.Name) {
			allErrs = append(allErrs, field.Duplicate(fldPath.Child("name"), sp.Name))
		} else {
			allNames.Insert(sp.Name)
		}
	}
	if sp.Protocol == api.ProtocolICMP {
		for _, msg := range validation.IsValidICMPPortRange(sp.Port, sp.PortRangeSize) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("port"), sp.Port, msg))
		}
	} else {
		allErrs = append(allErrs, ValidatePositiveField(sp.PortRangeSize, fldPath.Child("portRangeSize"))...)
		for _, msg := range validation.IsValidPortRange(sp.Port, sp.PortRangeSize) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("port"), sp.Port, msg))
		}
	}
	if sp.Protocol == api.ProtocolTCP && (sp.Port == RestAPIPort || sp.NodePort == RestAPIPort) {
		msg := fmt.Sprintf("a service cannot use port %d since it is used by Milpa", RestAPIPort)
		allErrs = append(allErrs, field.Invalid(fldPath.Child("port"), sp.Port, msg))
	}

	if len(sp.Protocol) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("protocol"), ""))
	} else if !supportedPortProtocols.Has(string(sp.Protocol)) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("protocol"), sp.Protocol, supportedPortProtocols.List()))
	}

	return allErrs
}

func ValidateLabelSelector(ps *api.LabelSelector, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if ps == nil {
		return allErrs
	}
	allErrs = append(allErrs, ValidateLabels(ps.MatchLabels, fldPath.Child("matchLabels"))...)
	for i, expr := range ps.MatchExpressions {
		allErrs = append(allErrs, ValidateLabelSelectorRequirement(expr, fldPath.Child("matchExpressions").Index(i))...)
	}
	return allErrs
}

func ValidateLabelSelectorRequirement(sr api.LabelSelectorRequirement, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	switch sr.Operator {
	case api.LabelSelectorOpIn, api.LabelSelectorOpNotIn:
		if len(sr.Values) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("values"), "must be specified when `operator` is 'In' or 'NotIn'"))
		}
	case api.LabelSelectorOpExists, api.LabelSelectorOpDoesNotExist:
		if len(sr.Values) > 0 {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("values"), "may not be specified when `operator` is 'Exists' or 'DoesNotExist'"))
		}
	default:
		allErrs = append(allErrs, field.Invalid(fldPath.Child("operator"), sr.Operator, "not a valid selector operator"))
	}
	allErrs = append(allErrs, ValidateLabelName(sr.Key, fldPath.Child("key"))...)
	return allErrs
}

func ValidateSecret(obj *api.Secret) field.ErrorList {
	allErrs := ValidateObjectMeta(&obj.ObjectMeta, true, ValidateSecretName, field.NewPath("metadata"))

	dataPath := field.NewPath("data")
	totalSize := 0
	for key, value := range obj.Data {
		for _, msg := range validation.IsConfigMapKey(key) {
			allErrs = append(allErrs, field.Invalid(dataPath.Key(key), key, msg))
		}
		totalSize += len(value)
	}
	if totalSize > MaxSecretSize {
		allErrs = append(allErrs, field.TooLong(dataPath, "", MaxSecretSize))
	}

	return allErrs
}

func validateVolumes(volumes []api.Volume, fldPath *field.Path) (sets.String, field.ErrorList) {
	allErrs := field.ErrorList{}

	allNames := sets.String{}
	for i, vol := range volumes {
		idxPath := fldPath.Index(i)
		namePath := idxPath.Child("name")
		el := validateVolumeSource(&vol.VolumeSource, idxPath)
		if len(vol.Name) == 0 {
			el = append(el, field.Required(namePath, ""))
		} else {
			el = append(el, ValidateDNS1123Label(vol.Name, namePath)...)
		}
		if allNames.Has(vol.Name) {
			el = append(el, field.Duplicate(namePath, vol.Name))
		}
		if len(el) == 0 {
			allNames.Insert(vol.Name)
		} else {
			allErrs = append(allErrs, el...)
		}
	}
	return allNames, allErrs
}

func validateVolumeSource(source *api.VolumeSource, fldPath *field.Path) field.ErrorList {
	numVolumes := 0
	allErrs := field.ErrorList{}
	if source.EmptyDir != nil {
		numVolumes++
		// EmptyDirs have nothing to validate
	}

	if source.PackagePath != nil {
		numVolumes++
		allErrs = append(allErrs, validatePackagePathVolumeSource(source.PackagePath, fldPath)...)
	}

	// we will likely implement secret volumes at some point

	// if source.Secret != nil {
	// 	if numVolumes > 0 {
	// 		allErrs = append(allErrs, field.Forbidden(fldPath.Child("secret"), "may not specify more than 1 volume type"))
	// 	} else {
	// 		numVolumes++
	// 		allErrs = append(allErrs, validateSecretVolumeSource(source.Secret, fldPath.Child("secret"))...)
	// 	}
	// }
	if numVolumes == 0 {
		allErrs = append(allErrs, field.Required(fldPath, "must specify a valid volume type"))
	}
	if numVolumes > 1 {
		allErrs = append(allErrs, field.Required(fldPath, "multiple volumes specified"))
	}

	return allErrs
}

func validatePackagePathVolumeSource(packagePath *api.PackagePath, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(packagePath.Path) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("packagePath.path"), ""))
	}
	return allErrs
}

// func validateSecretVolumeSource(secretSource *api.SecretVolumeSource, fldPath *field.Path) field.ErrorList {
// 	allErrs := field.ErrorList{}
// 	if len(secretSource.SecretName) == 0 {
// 		allErrs = append(allErrs, field.Required(fldPath.Child("secretName"), ""))
// 	}
// 	return allErrs
// }

func validateVolumeMounts(mounts []api.VolumeMount, volumes sets.String, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	mountpoints := sets.NewString()

	for i, mnt := range mounts {
		idxPath := fldPath.Index(i)
		if len(mnt.Name) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("name"), ""))
		} else if !volumes.Has(mnt.Name) {
			allErrs = append(allErrs, field.NotFound(idxPath.Child("name"), mnt.Name))
		}
		if len(mnt.MountPath) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("mountPath"), ""))
		} else if strings.Contains(mnt.MountPath, ":") {
			allErrs = append(allErrs, field.Invalid(idxPath.Child("mountPath"), mnt.MountPath, "must not contain ':'"))
		}
		if mountpoints.Has(mnt.MountPath) {
			allErrs = append(allErrs, field.Invalid(idxPath.Child("mountPath"), mnt.MountPath, "must be unique"))
		}
		mountpoints.Insert(mnt.MountPath)
	}
	return allErrs
}

func validateNonSpecialIP(ipAddress string, fldPath *field.Path) field.ErrorList {
	// We disallow some IPs as endpoints or external-ips.  Specifically,
	// unspecified and loopback addresses are nonsensical and link-local
	// addresses tend to be used for node-centric purposes (e.g. metadata
	// service).
	allErrs := field.ErrorList{}
	ip := net.ParseIP(ipAddress)
	if ip == nil {
		allErrs = append(allErrs, field.Invalid(fldPath, ipAddress, "must be a valid IP address"))
		return allErrs
	}
	if ip.IsUnspecified() {
		allErrs = append(allErrs, field.Invalid(fldPath, ipAddress, "may not be unspecified (0.0.0.0)"))
	}
	if ip.IsLoopback() {
		allErrs = append(allErrs, field.Invalid(fldPath, ipAddress, "may not be in the loopback range (127.0.0.0/8)"))
	}
	if ip.IsLinkLocalUnicast() {
		allErrs = append(allErrs, field.Invalid(fldPath, ipAddress, "may not be in the link-local range (169.254.0.0/16)"))
	}
	if ip.IsLinkLocalMulticast() {
		allErrs = append(allErrs, field.Invalid(fldPath, ipAddress, "may not be in the link-local multicast range (224.0.0.0/24)"))
	}
	return allErrs
}
