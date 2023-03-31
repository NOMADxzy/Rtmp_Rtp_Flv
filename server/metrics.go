package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

// 定义指标
var (
	registry = prometheus.NewRegistry()
	// 统计请求数量
	streamNumberCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Subsystem: "service",
			Name:      "rtmp_stream_num",
			Help:      "Total number of rtmp_stream",
		},
	)

	streamCacheUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: "service",
			Name:      "rtmp_stream_cache_usage",
			Help:      "rtmp stream cache usage",
		},
		[]string{"stream"},
	)
)

func initMetrics() {
	registry.MustRegister(streamNumberCount)
	registry.MustRegister(streamCacheUsage)
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	registry.MustRegister(prometheus.NewGoCollector())

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	go http.ListenAndServe(":8080", nil)
	fmt.Printf("metrics api listening on %v\n", ":8080")
}
