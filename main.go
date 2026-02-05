package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Node struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	CPU    float64 `json:"cpu"`
	RAM    int64   `json:"ram"`
	PIDs   int     `json:"pids"`
	NetRaw int64   `json:"net_raw"`
	Frozen bool    `json:"frozen"`
}

type MetricUpdate struct {
	Location string `json:"location"`
	Nodes    []Node `json:"nodes"`
}

const cgroupRoot = "/sys/fs/cgroup"

var lastCPUTimes = make(map[string]int64)
var lastNetTotal int64

func getNetDelta() int64 {
	data, _ := os.ReadFile("/proc/net/dev")
	lines := strings.Split(string(data), "\n")
	var current int64
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) > 1 && (strings.HasPrefix(f[0], "en") || strings.HasPrefix(f[0], "wl") || strings.HasPrefix(f[0], "eth")) {
			b, _ := strconv.ParseInt(f[1], 10, 64)
			current += b
		}
	}
	delta := current - lastNetTotal
	lastNetTotal = current
	return delta
}

func createNode(path string, netDelta int64) Node {
	d, _ := os.ReadFile(filepath.Join(path, "cpu.stat"))
	var usage int64
	for _, l := range strings.Split(string(d), "\n") {
		if strings.HasPrefix(l, "usage_usec") {
			f := strings.Fields(l)
			if len(f) > 1 {
				usage, _ = strconv.ParseInt(f[1], 10, 64)
			}
		}
	}
	pct := 0.0
	if prev, ok := lastCPUTimes[path]; ok {
		pct = float64(usage-prev) / 2000.0 // 200ms scale
	}
	lastCPUTimes[path] = usage

	memD, _ := os.ReadFile(filepath.Join(path, "memory.current"))
	mem, _ := strconv.ParseInt(strings.TrimSpace(string(memD)), 10, 64)
	pids, _ := os.ReadFile(filepath.Join(path, "cgroup.procs"))
	frz, _ := os.ReadFile(filepath.Join(path, "cgroup.freeze"))

	return Node{
		Name: filepath.Base(path), Path: path,
		CPU: pct, RAM: mem / 1024 / 1024,
		PIDs:   len(strings.Fields(string(pids))),
		NetRaw: netDelta,
		Frozen: strings.TrimSpace(string(frz)) == "1",
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	cur, q, focus := cgroupRoot, "", ""
	go func() {
		for {
			var cmd map[string]string
			if err := conn.ReadJSON(&cmd); err != nil {
				break
			}
			if p, ok := cmd["path"]; ok {
				cur = p
				q = ""
			}
			if s, ok := cmd["search"]; ok {
				q = s
			}
			if f, ok := cmd["focus"]; ok {
				focus = f
			} // New 'focus' command
			if act, ok := cmd["action"]; ok {
				val := "0"
				if act == "freeze" {
					val = "1"
				}
				os.WriteFile(filepath.Join(cmd["target"], "cgroup.freeze"), []byte(val), 0644)
			}
		}
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	for range ticker.C {
		net := getNetDelta()
		var nodes []Node

		// 1. If focusing a specific node (Inspector Page)
		if focus != "" {
			nodes = append(nodes, createNode(focus, net))
		}

		// 2. Regular Browsing/Search
		if q != "" {
			filepath.Walk(cgroupRoot, func(p string, i os.FileInfo, e error) error {
				if e == nil && i.IsDir() && strings.Contains(strings.ToLower(i.Name()), strings.ToLower(q)) {
					if p != cgroupRoot && p != focus {
						nodes = append(nodes, createNode(p, net))
					}
				}
				return nil
			})
		} else {
			ents, _ := os.ReadDir(cur)
			for _, e := range ents {
				p := filepath.Join(cur, e.Name())
				if e.IsDir() && p != focus {
					nodes = append(nodes, createNode(p, net))
				}
			}
		}

		rel, _ := filepath.Rel(cgroupRoot, cur)
		if err := conn.WriteJSON(MetricUpdate{"/" + rel, nodes}); err != nil {
			break
		}
	}
}

func main() {
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/ws", wsHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
