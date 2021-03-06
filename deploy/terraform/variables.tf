variable "ssh-key-name" {
  default = ""
}

variable "cluster-name" {
  default = "vk"
}

variable "region" {
  default = "us-east-1"
}

variable "vpc-cidr" {
  default = "10.0.0.0/16"
}

variable "pod-cidr" {
  default = "172.20.0.0/16"
}

variable "service-cidr" {
  default = "10.96.0.0/12"
}

variable "k8s-version" {
  default = ""
}

variable "node-disk-size" {
  default = 15
}

variable "blacklisted-azs" {
  type    = list(string)
  default = ["use1-az3"]
}

variable "node-ami" {
  default = ""
}

variable "kustomize-dir" {
  default = "../manifests/virtual-kubelet/base"
}
