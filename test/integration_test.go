package test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

var N = 5

func InitNodes(t *testing.T) []*Node {
	KillPorts(N)
	nodes := NewNodes(N)
	StartNodes(t, nodes, "true")

	return nodes
}

func TestElection(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	leader, leaderCount := CountLeader(t, nodes)
	if leaderCount != 1 {
		t.Fatalf("expected 1 leader, got %d", leaderCount)
	}

	t.Logf("%s has been killed", leader.id)
	leader.StopNode()
	WaitForLeader(t, nodes, 10*time.Second)

	_, leaderCount = CountLeader(t, nodes)
	if leaderCount != 1 {
		t.Fatalf("expected 1 leader, got %d", leaderCount)
	}
}

func TestLogReplication(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	nodes[1].Put(t, "key1", "value1")

	for _, node := range nodes {
		val := node.Get(t, "key1")
		//t.Logf("%s returned raw: [%s]\n", node.id, val)
		if val != "value1" {
			t.Fatalf("%s has wrong value: %s", node.id, val)
		}
	}
}

func Test100LogReplication(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	for i := 1; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		nodes[rand.Intn(len(nodes))].Put(t, key, value)
	}

	for _, node := range nodes {
		for i := 1; i < 100; i++ {
			key := fmt.Sprintf("key%d", i)
			expectedValue := fmt.Sprintf("value%d", i)
			value := node.Get(t, key)

			//t.Logf("%s returned raw: [%s]\n", node.id, val)
			if value != expectedValue {
				t.Fatalf("%s has wrong value: %s", node.id, value)
			}
		}

	}
}

func TestLogPersistence(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		nodes[rand.Intn(len(nodes))].Put(t, key, value)
	}

	WaitForValue(t, nodes, "key9", "value9", 10*time.Second)

	for _, node := range nodes {
		t.Logf("Killing node %s", node.id)
		node.StopNode()
		t.Logf("Restarting node %s", node.id)
		node.StartNode(t, "false")
		WaitForValue(t, []*Node{node}, "key9", "value9", 10*time.Second)
	}

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		value := nodes[rand.Intn(len(nodes))].Get(t, key)

		if value != expectedValue {
			t.Fatalf("expected %s but got wrong value: %s", expectedValue, value)
		}
	}
}

func TestMissedLogsRecovery(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	nodes[0].StopNode()
	WaitForLeader(t, nodes[1:], 10*time.Second)

	activeNodes := nodes[1:]
	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		activeNodes[rand.Intn(len(activeNodes))].Put(t, key, value)
		WaitForValue(t, activeNodes, key, value, 15*time.Second)
	}

	nodes[0].StartNode(t, "false")
	WaitForLeader(t, nodes, 10*time.Second)
	WaitForValue(t, []*Node{nodes[0]}, "key9", "value9", 20*time.Second)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		value := nodes[0].Get(t, key)

		if value != expectedValue {
			t.Fatalf("expected %s but got wrong value: %s", expectedValue, value)
		}
	}
}

// sustained churn: random stop/start of followers while issuing writes
func TestFollowerChurnUnderLoad(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	leader, _ := CountLeader(t, nodes)

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		val := fmt.Sprintf("v%d", i)
		nodes[rand.Intn(N)].Put(t, key, val)
		WaitForValue(t, nodes, key, val, 10*time.Second)

		f := nodes[rand.Intn(N)]
		if f != leader {
			f.StopNode()
			f.StartNode(t, "false")
			WaitForValue(t, []*Node{f}, key, val, 10*time.Second)
		}
	}

	// validate data on every node
	for _, n := range nodes {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("k%d", i)
			want := fmt.Sprintf("v%d", i)
			if got := n.Get(t, key); got != want {
				t.Fatalf("%s wrong value for %s: got %s", n.id, key, got)
			}
		}
	}
}

func TestNetworkPartition(t *testing.T) {
	nodes := InitNodes(t)
	defer StopNodes(nodes)

	partition1 := nodes[:2]
	partition2 := nodes[2:]

	for _, node := range partition1 {
		node.StopNode()
	}

	WaitForLeader(t, partition2, 10*time.Second)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		partition2[rand.Intn(len(partition2))].Put(t, key, value)
	}

	for _, node := range partition1 {
		node.StartNode(t, "false")
	}
	WaitForValue(t, nodes, "key9", "value9", 10*time.Second)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		for _, node := range nodes {
			value := node.Get(t, key)
			if value != expectedValue {
				t.Fatalf("%s has wrong value: %s", node.id, value)
			}
		}
	}
}
