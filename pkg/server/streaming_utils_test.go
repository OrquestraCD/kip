/*
Copyright 2020 Elotl Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"testing"

	"github.com/elotl/kip/pkg/api"
	"github.com/elotl/kip/pkg/server/registry"
	"github.com/stretchr/testify/assert"
)

func setupServer() (InstanceProvider, func()) {
	podReg, podCloser := registry.SetupTestPodRegistry()
	s := InstanceProvider{
		Registries: map[string]registry.Registryer{
			"Pod": podReg,
		},
	}
	nodeReg, nodeCloser := registry.SetupTestNodeRegistry()
	s.Registries["Node"] = nodeReg
	return s, func() {
		podCloser()
		nodeCloser()
	}
}

func TestGetNodeForRunningPod(t *testing.T) {
	s, closer := setupServer()
	defer closer()
	podReg := s.Registries["Pod"].(*registry.PodRegistry)
	nodeReg := s.Registries["Node"].(*registry.NodeRegistry)

	_, err := s.GetNodeForRunningPod("NotInReg", "")
	assert.Error(t, err)

	pod := api.GetFakePod()
	_, err = podReg.CreatePod(pod)
	assert.NoError(t, err)
	_, err = s.GetNodeForRunningPod(pod.Name, "")
	assert.Error(t, err)

	runningPod := api.GetFakePod()
	runningPod.Status.Phase = api.PodRunning

	_, err = podReg.CreatePod(runningPod)
	assert.NoError(t, err)
	_, err = s.GetNodeForRunningPod(runningPod.Name, "")
	assert.Error(t, err)

	node := api.GetFakeNode()
	_, err = nodeReg.CreateNode(node)
	assert.NoError(t, err)
	runningPod.Status.BoundNodeName = node.Name
	_, err = podReg.UpdatePodStatus(runningPod, "")
	assert.NoError(t, err)
	n, err := s.GetNodeForRunningPod(runningPod.Name, "")
	assert.NoError(t, err)
	assert.Equal(t, n.Name, node.Name)

	_, err = s.GetNodeForRunningPod(pod.Name, "notinpod")
	assert.Error(t, err)
}
