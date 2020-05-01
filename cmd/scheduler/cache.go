package main

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"log"
	"strconv"
	"time"
)

/* Current resources associated to each node */
type resources struct {
	cpu    int /* number of cores */
	memory int /* memory in GB */
	cost   int /* 0 - lowest, +INF - greatest */
}

/* Map from nodeName to resources */
type nodeCache struct {
	cache map[string]resources
}

func (c *nodeCache) getNodes() []string {
	return getKeys(c.cache)
}

func (c *nodeCache) podFits(podCPU string, podMEM string) bool {
	for _, value := range c.cache {
		if value.cpu-toInt(podCPU) > 0 && value.memory-toInt(podMEM) > 0 { /* We still need 1 CPU for kubelet */
			return true
		}
	}
	return false
}

func (c *nodeCache) multiplePodsFit(podCount string, podCPU string, podMEM string) bool {
	cpu := toInt(podCPU)
	mem := toInt(podMEM)
	count := toInt(podCount)

	workingCache := c.clone()

	//log.Println("Let's see how initial working cache looks like: ")
	//workingCache.printCache()

	for i := 0; i < count; i++ {
		if !workingCache.subtractPod(cpu, mem) {
			log.Println("Cannot schedule this executor with ID = ", i, " because the cluster is FULL")
			return false
		}
	}

	//log.Println("Working cache after all subtractions: ")
	//workingCache.printCache()
	//
	//log.Println("The actual cache should be untouched: ")
	//c.printCache()

	return true
}

func (c *nodeCache) subtractPod(cpu int, mem int) bool {
	for key, value := range c.cache {
		if value.cpu-cpu > 0 && value.memory-mem > 0 {
			c.cache[key] = resources{
				cpu:    value.cpu - cpu,
				memory: value.memory - mem,
				cost:   value.cost,
			}
			return true
		}
	}
	return false
}

func (c *nodeCache) schedulePod(cpu string, mem string) string {
	var bestNode string
	var minCost = 100

	for key, value := range c.cache {
		if value.cost < minCost && (value.cpu-toInt(cpu) > 0 && value.memory-toInt(mem) > 0) {
			bestNode = key
			minCost = value.cost
		}
	}

	c.cache[bestNode] = resources{
		cpu:    c.cache[bestNode].cpu - toInt(cpu),
		memory: c.cache[bestNode].memory - toInt(mem),
		cost:   minCost,
	}

	//log.Println("Updated production cache (after the actual scheduling): ")
	//c.printCache()
	//
	//log.Println("Chosen node: ", bestNode)

	return bestNode
}

func createNodeCache(lister listersv1.NodeLister) nodeCache {
	cache := nodeCache{
		cache: buildResourceMap(lister),
	}

	log.Println("Let's see how the initial production cache looks like: ")
	cache.printCache()

	return cache
}

func buildResourceMap(lister listersv1.NodeLister) map[string]resources {
	nodes := getNodeList(lister)
	result := make(map[string]resources)

	for _, node := range nodes {
		nodeCost, _ := strconv.Atoi(node.Labels[NODE_COST_LABEL_KEY])
		result[node.Name] = resources{
			cpu:    int(node.Status.Allocatable.Cpu().Value()),
			memory: getMemoryInGb(node.Status.Allocatable.Memory().Value()),
			cost:   nodeCost,
		}
	}
	return result
}

/* Wait for node informer to warm up. If 4 successive calls return
   the same number of nodes, then the informer is warmed up */
func getNodeList(lister listersv1.NodeLister) []*v1.Node {
	var result1, result2 []*v1.Node

	counter := 1
	warmingThreshold := 4

	for {
		result1, _ = lister.List(labels.Everything())
		time.Sleep(1 * time.Second)
		result2, _ = lister.List(labels.Everything())

		if len(result1) == len(result2) {
			counter++
		}

		if counter == warmingThreshold {
			return result1
		}

		log.Println("Warming node informer...")
	}
}
