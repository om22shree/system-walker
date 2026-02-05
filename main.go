package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type MetricUpdate struct {
	Location  string `json:"location"`
	Cores     int    `json:"cores"`
	Nodes     []Node `json:"nodes"`
	Timestamp string `json:"timestamp"`
}

type Node struct {
	Name string `json:"name"`
	Path string `json:"path"`
	CPU  string `json:"cpu"`
	RAM  string `json:"ram"`
	PIDs int    `json:"pids"`
}

const cgroupRoot = "/sys/fs/cgroup"

var lastCPUTimes = make(map[string]int64)

func getSystemMetrics(currentPath string) MetricUpdate {
	var nodes []Node
	entries, _ := os.ReadDir(currentPath)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(currentPath, entry.Name())

		cpuUsage := getCPUUsage(path)
		cpuPercent := 0.0
		if prev, ok := lastCPUTimes[path]; ok {
			cpuPercent = float64(cpuUsage-prev) / 1000000.0 * 100 / float64(runtime.NumCPU())
		}
		lastCPUTimes[path] = cpuUsage

		memData, _ := os.ReadFile(filepath.Join(path, "memory.current"))
		memRaw, _ := strconv.ParseInt(strings.TrimSpace(string(memData)), 10, 64)

		pidData, _ := os.ReadFile(filepath.Join(path, "cgroup.procs"))
		pidCount := len(strings.Fields(string(pidData)))

		nodes = append(nodes, Node{
			Name: entry.Name(),
			Path: path,
			CPU:  fmt.Sprintf("%.1f%%", cpuPercent),
			RAM:  fmt.Sprintf("%d MB", memRaw/1024/1024),
			PIDs: pidCount,
		})
	}

	rel, _ := filepath.Rel(cgroupRoot, currentPath)
	return MetricUpdate{
		Location:  "/" + rel,
		Cores:     runtime.NumCPU(),
		Nodes:     nodes,
		Timestamp: time.Now().Format("15:04:05"),
	}
}

func getCPUUsage(path string) int64 {
	data, _ := os.ReadFile(filepath.Join(path, "cpu.stat"))
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "usage_usec") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				val, _ := strconv.ParseInt(parts[1], 10, 64)
				return val
			}
		}
	}
	return 0
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	currentPath := cgroupRoot

	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			newPath := string(msg)
			if strings.HasPrefix(newPath, cgroupRoot) {
				currentPath = newPath
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		metrics := getSystemMetrics(currentPath)
		if err := conn.WriteJSON(metrics); err != nil {
			break
		}
	}
}

func main() {
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/ws", wsHandler)
	fmt.Println("System Walker Server active on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
