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

package main

import "github.com/spf13/pflag"

type ServerConfig struct {
	DebugServer        bool
	NetworkAgentSecret string
}

func (c *ServerConfig) FlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("serverconfig", pflag.ContinueOnError)
	flags.BoolVar(&c.DebugServer, "debug-server", c.DebugServer, "Enable a listener in the server for inspecting internal kip structures.")
	flags.StringVar(&c.NetworkAgentSecret, "network-agent-secret", c.NetworkAgentSecret, "Service account secret for the cell network agent, in the form of <namespace>/<name>")
	return flags
}
