// file: internal/metrics/collector.go

package metrics

import (
	"sync"
	"time"
)

// Collector periodically refreshes the process gauges (goroutines, memory).
type Collector struct {
	metrics        *Metrics
	updateInterval time.Duration
	stopChan       chan struct{}
	wg             sync.WaitGroup
}

// NewCollector creates a metrics collector that ticks at updateInterval.
func NewCollector(metrics *Metrics, updateInterval time.Duration) *Collector {
	return &Collector{
		metrics:        metrics,
		updateInterval: updateInterval,
		stopChan:       make(chan struct{}),
	}
}

// Start begins periodic collection of system metrics.
func (c *Collector) Start() {
	c.wg.Add(1)
	go c.collect()
}

// Stop gracefully shuts down the collector.
func (c *Collector) Stop() {
	close(c.stopChan)
	c.wg.Wait()
}

func (c *Collector) collect() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.metrics.UpdateSystemMetrics()
		}
	}
}
