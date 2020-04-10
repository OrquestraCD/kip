variable "region" {
  type        = string
  default     = "us-east-1"
  description = "The AWS region to use."
}

variable "aws_access_key_id" {
  type        = string
  description = "The AWS access key id Kip will use for creating cells."
}

variable "aws_secret_access_key" {
  type        = string
  description = "The AWS secret access key Kip will use for creating cells."
}

variable "name" {
  type        = string
  default     = "cloud-burst"
  description = "A name that will be used to tag AWS resources."
}

variable "client_ip" {
  type        = string
  default     = ""
  description = "The VPN connection needs a source IP. If left empty, it will be auto-detected."
}

variable "vpc_cidr" {
  type        = string
  default     = "10.10.0.0/16"
  description = "The CIDR to use for the VPC."
}

variable "azs" {
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b", "us-east-1c"]
  description = "Availability zones used for subnets in the VPC."
}

variable "local_cidrs" {
  type        = list(string)
  default     = ["192.168.0.0/16", "172.16.0.0/12", "10.0.2.0/24"]
  description = "This CIDRs will be routed back from the VPC via the VPN connection."
}

variable "tunnel1_inside_cidr" {
  type        = string
  default     = "169.254.10.20/30"
  description = "A link-local /30 CIDR that will be used for the first VPN tunnel."
}

variable "tunnel2_inside_cidr" {
  type        = string
  default     = "169.254.30.40/30"
  description = "A link-local /30 CIDR that will be used for the second VPN tunnel."
}

variable "tunnel1_psk" {
  type        = string
  description = "The pre-shared key for the first VPN tunnel."
}

variable "tunnel2_psk" {
  type        = string
  description = "The pre-shared key for the second VPN tunnel."
}

variable "deploy_to_kubernetes" {
  type        = bool
  default     = true
  description = "Whether the generated Kubernetes resources will be applied via kubectl. Disable if you only need the kustomization/ directory generated, and you plan to apply it separately. If enabled, it needs kubectl >= 1.14."
}

variable "cluster_domain" {
  type = string
  default = "cluster.local"
  description = "The Kubernetes cluster domain, used by pods. See https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/ for more information."
}

variable "kube_dns" {
  type = string
  default = "10.96.0.10"
  description = "The clusterIP of the main DNS service (usually called kube-dns) in Kubernetes. See https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/ for more information."
}

variable "local_dns" {
  type = string
  default = "169.254.10.20"
  description = "The IP address used for the local DNS cache pod that will be launched in the VPC to improve DNS performance. See https://kubernetes.io/docs/tasks/administer-cluster/nodelocaldns/ on DNS caching."
}
