package main

import (
	"log"
	"math"
	"strconv"
)

func getMemoryInGb(memoryValue int64) int {
	return int(math.Floor(float64(memoryValue / (1024 * 1024 * 1024))))
}

func getKeys(resourcesMap map[string]resources) []string {
	result := make([]string, 0, len(resourcesMap))
	for key, _ := range resourcesMap {
		result = append(result, key)
	}
	return result
}

func toInt(value string) int {
	result, _ := strconv.Atoi(value)
	return result
}

func (c *nodeCache) clone() nodeCache {
	clone := nodeCache{cache:make(map[string]resources)}

	for key, value := range c.cache {
		clone.cache[key] = value
	}

	log.Println("Let's see how the clone looks like: ")
	clone.printCache()

	return clone
}

func (c *nodeCache) printCache() {
	for key, value := range c.cache {
		log.Println("Key = ", key, "value = ", value)
	}
}