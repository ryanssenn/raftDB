//go:build stress

package test

import "testing"

func TestIntegrationStress(t *testing.T) {
	Test100LogReplication(t)
	TestFollowerChurnUnderLoad(t)
	TestNetworkPartition(t)
}
