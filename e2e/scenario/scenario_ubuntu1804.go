package scenario

import "github.com/Azure/agentbakere2e/config"

// Returns config for the 'base' E2E scenario
func ubuntu1804() *Scenario {
	return &Scenario{
		Name:        "ubuntu1804",
		Description: "Tests that a node using an Ubuntu 1804 VHD can be properly bootstrapped",
		Config: Config{
			ClusterSelector: NetworkPluginKubenetSelector,
			ClusterMutator:  NetworkPluginKubenetMutator,
			VHDSelector:     config.VHDUbuntu1804Gen2Containerd,
		},
	}
}
