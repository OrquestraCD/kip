### Cells

Cells are the name we’ve given to the cloud instances that kip runs pods on.  The cells run Alpine linux and a lightweight agent called [Itzo](https://github.com/elotl/itzo) that performs the task of running pods on the cells.  More information about cells can be found in the itzo repository.

To boot cells, unless an instance-type annotation is present in the pod, kip will choose the cheapest instance type that satisfies the resource requirements of the pod and will fall back to the `defaultInstanceType` in the `virutal-kubelet-provider.yml` if no resources are specified.  Server certificates are passed to the cell via instance user data and those certificates allow the kip controller to connect to the cell.

Cells can be customized by specifying a [cloud-init](cloud-init.md) file that will be applied when the instance boots.