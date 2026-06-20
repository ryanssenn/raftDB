package test

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

var N = 5

func TestElection(t *testing.T) {
	nodes := InitNodes(t)

	leader, leaderCount := CountLeader(t, nodes)
	if leaderCount != 1 {
		t.Fatalf("expected 1 leader, got %d", leaderCount)
	}

	t.Logf("%s has been killed", leader.id)
	leader.StopNode()
	WaitForLeader(t, nodes, 15*time.Second)

	_, leaderCount = CountLeader(t, nodes)
	if leaderCount != 1 {
		t.Fatalf("expected 1 leader, got %d", leaderCount)
	}
}

func TestLogReplication(t *testing.T) {
	nodes := InitNodes(t)

	nodes[1].PutMustSucceed(t, "key1", "value1")
	WaitForValue(t, nodes, "key1", "value1", 15*time.Second)

	for _, node := range nodes {
		val := node.Get(t, "key1")
		if val != "value1" {
			t.Fatalf("%s has wrong value: %s", node.id, val)
		}
	}
}

func Test100LogReplication(t *testing.T) {
	nodes := InitNodes(t)

	for i := 1; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		nodes[rand.Intn(len(nodes))].PutMustSucceed(t, key, value)
	}

	for _, node := range nodes {
		for i := 1; i < 100; i++ {
			key := fmt.Sprintf("key%d", i)
			expectedValue := fmt.Sprintf("value%d", i)
			value := node.Get(t, key)
			if value != expectedValue {
				t.Fatalf("%s has wrong value: %s", node.id, value)
			}
		}
	}
}

func TestLogPersistence(t *testing.T) {
	nodes := InitNodes(t)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		nodes[rand.Intn(len(nodes))].PutMustSucceed(t, key, value)
	}
	WaitForValue(t, nodes, "key9", "value9", 30*time.Second)

	leader, _ := CountLeader(t, nodes)
	restartOrder := make([]*Node, 0, len(nodes))
	for _, node := range nodes {
		if node != leader {
			restartOrder = append(restartOrder, node)
		}
	}
	if leader != nil {
		restartOrder = append(restartOrder, leader)
	}

	for _, node := range restartOrder {
		t.Logf("Killing node %s", node.id)
		node.StopNode()
		WaitForNodeDown(t, node, 10*time.Second)
		t.Logf("Restarting node %s", node.id)
		node.StartNode(t, "false")
		WaitForLeader(t, nodes, 30*time.Second)
		WaitForValue(t, nodes, "key9", "value9", 60*time.Second)
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

	nodes[0].StopNode()
	WaitForLeader(t, nodes[1:], 15*time.Second)

	activeNodes := nodes[1:]
	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		activeNodes[rand.Intn(len(activeNodes))].PutMustSucceed(t, key, value)
		WaitForValue(t, activeNodes, key, value, 20*time.Second)
	}

	nodes[0].StartNode(t, "false")
	WaitForLeader(t, nodes, 15*time.Second)
	WaitForValue(t, []*Node{nodes[0]}, "key9", "value9", 30*time.Second)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		value := nodes[0].Get(t, key)
		if value != expectedValue {
			t.Fatalf("expected %s but got wrong value: %s", expectedValue, value)
		}
	}
}

func TestFollowerChurnUnderLoad(t *testing.T) {
	nodes := InitNodes(t)

	leader, _ := CountLeader(t, nodes)

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k%d", i)
		val := fmt.Sprintf("v%d", i)
		nodes[rand.Intn(N)].PutMustSucceed(t, key, val)
		WaitForValue(t, nodes, key, val, 15*time.Second)

		f := nodes[rand.Intn(N)]
		if f != leader {
			f.StopNode()
			f.StartNode(t, "false")
			WaitForValue(t, []*Node{f}, key, val, 15*time.Second)
		}
		leader, _ = CountLeader(t, nodes)
	}

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

	partition1 := nodes[:2]
	partition2 := nodes[2:]

	for _, node := range partition1 {
		node.StopNode()
	}

	WaitForLeader(t, partition2, 15*time.Second)

	for i := 1; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		partition2[rand.Intn(len(partition2))].PutMustSucceed(t, key, value)
		WaitForValue(t, partition2, key, value, 15*time.Second)
	}

	for _, node := range partition1 {
		node.StartNode(t, "false")
	}
	WaitForLeader(t, nodes, 15*time.Second)
	WaitForValue(t, nodes, "key9", "value9", 20*time.Second)

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

func TestNoDualLeaders(t *testing.T) {
	nodes := InitNodes(t)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		termLeaders := map[int64]int{}
		for _, node := range nodes {
			status, err := node.TryStatus()
			if err != nil || status.State != 2 {
				continue
			}
			termLeaders[status.Term]++
			if termLeaders[status.Term] > 1 {
				dumpNodeStatuses(t, nodes)
				t.Fatalf("observed %d leaders in term %d", termLeaders[status.Term], status.Term)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestWriteWhileNoLeader(t *testing.T) {
	nodes := InitNodes(t)

	for _, node := range nodes[:3] {
		node.StopNode()
	}
	for _, node := range nodes[:3] {
		WaitForNodeDown(t, node, 10*time.Second)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := nodes[3].TryPut("key", "value")
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if resp != "success" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("expected put to fail while majority is down, but got success")
}

func TestConcurrentWrites(t *testing.T) {
	nodes := InitNodes(t)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				key := fmt.Sprintf("g%d-k%d", gid, i)
				val := fmt.Sprintf("v%d-%d", gid, i)
				nodes[rand.Intn(len(nodes))].PutMustSucceed(t, key, val)
			}
		}(g)
	}
	wg.Wait()

	for g := 0; g < 10; g++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("g%d-k%d", g, i)
			want := fmt.Sprintf("v%d-%d", g, i)
			WaitForValue(t, nodes, key, want, 20*time.Second)
		}
	}
}
