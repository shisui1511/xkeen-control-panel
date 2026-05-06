package handlers

import (
	"bufio"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type SystemStats struct {
	Memory struct {
		Total uint64 `json:"total"`
		Used  uint64 `json:"used"`
		Free  uint64 `json:"free"`
	} `json:"memory"`
	Load   [3]float64 `json:"load"`
	Uptime struct {
		Seconds float64 `json:"seconds"`
		Days    int     `json:"days"`
		Hours   int     `json:"hours"`
		Minutes int     `json:"minutes"`
	} `json:"uptime"`
	GoRuntime struct {
		Goroutines int    `json:"goroutines"`
		HeapAlloc  uint64 `json:"heap_alloc"`
		HeapSys    uint64 `json:"heap_sys"`
		NumGC      uint32 `json:"num_gc"`
	} `json:"go_runtime"`
}

func (a *API) SystemStats(w http.ResponseWriter, r *http.Request) {
	stats := SystemStats{}

	// Memory from /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			key := strings.TrimSuffix(fields[0], ":")
			val, _ := strconv.ParseUint(fields[1], 10, 64)
			val *= 1024 // kB to bytes

			switch key {
			case "MemTotal":
				stats.Memory.Total = val
			case "MemAvailable":
				stats.Memory.Free = val
			}
		}
		stats.Memory.Used = stats.Memory.Total - stats.Memory.Free
	}

	// Load average from /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			stats.Load[0], _ = strconv.ParseFloat(fields[0], 64)
			stats.Load[1], _ = strconv.ParseFloat(fields[1], 64)
			stats.Load[2], _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// Uptime from /proc/uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			seconds, _ := strconv.ParseFloat(fields[0], 64)
			stats.Uptime.Seconds = seconds
			stats.Uptime.Days = int(seconds) / 86400
			stats.Uptime.Hours = (int(seconds) % 86400) / 3600
			stats.Uptime.Minutes = (int(seconds) % 3600) / 60
		}
	}

	// Go runtime stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	stats.GoRuntime.Goroutines = runtime.NumGoroutine()
	stats.GoRuntime.HeapAlloc = m.HeapAlloc
	stats.GoRuntime.HeapSys = m.HeapSys
	stats.GoRuntime.NumGC = m.NumGC

	a.jsonResponse(w, stats)
}
