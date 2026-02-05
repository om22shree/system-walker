package main

import (
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Node struct {
	Name string  `json:"name"`
	Path string  `json:"path"`
	ID   string  `json:"id"`
	CPU  float64 `json:"cpu"`
	RAM  int64   `json:"ram"`
}

type GlobalState struct {
	sync.RWMutex
	AllNodes map[string]Node
}

var (
	State        = &GlobalState{AllNodes: make(map[string]Node)}
	lastCPUTimes = make(map[string]int64)
	cgroupRoot   = "/sys/fs/cgroup"
)

func startScanner() {
	ticker := time.NewTicker(200 * time.Millisecond)
	for range ticker.C {
		tempNodes := make(map[string]Node)
		filepath.Walk(cgroupRoot, func(p string, i os.FileInfo, e error) error {
			if e == nil && i.IsDir() {
				d, _ := os.ReadFile(filepath.Join(p, "cpu.stat"))
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
				if prev, ok := lastCPUTimes[p]; ok {
					pct = float64(usage-prev) / 2000.0
				}
				lastCPUTimes[p] = usage

				memD, _ := os.ReadFile(filepath.Join(p, "memory.current"))
				mem, _ := strconv.ParseInt(strings.TrimSpace(string(memD)), 10, 64)

				clean := filepath.Clean(p)
				tempNodes[clean] = Node{
					Name: i.Name(),
					Path: clean,
					ID:   base64.StdEncoding.EncodeToString([]byte(clean)),
					CPU:  pct,
					RAM:  mem / 1024 / 1024,
				}
			}
			return nil
		})
		State.Lock()
		State.AllNodes = tempNodes
		State.Unlock()
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	cur, q, focusID := cgroupRoot, "", ""
	go func() {
		for {
			var cmd map[string]string
			if err := conn.ReadJSON(&cmd); err != nil {
				break
			}
			if p, ok := cmd["path"]; ok {
				cur = filepath.Clean(p)
				q = ""
			}
			if s, ok := cmd["search"]; ok {
				q = strings.ToLower(s)
			}
			if f, ok := cmd["focus"]; ok {
				focusID = f
			}
		}
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	for range ticker.C {
		var output []Node
		State.RLock()
		if focusID != "" {
			for _, n := range State.AllNodes {
				if n.ID == focusID {
					output = append(output, n)
					break
				}
			}
		} else if q != "" {
			for _, n := range State.AllNodes {
				if strings.Contains(strings.ToLower(n.Name), q) {
					output = append(output, n)
				}
			}
		} else {
			for p, n := range State.AllNodes {
				if filepath.Dir(p) == cur {
					output = append(output, n)
				}
			}
		}
		State.RUnlock()

		rel, _ := filepath.Rel(cgroupRoot, cur)
		conn.WriteJSON(map[string]interface{}{"location": "/" + rel, "nodes": output})
	}
}

func main() {
	go startScanner()
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/ws", wsHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
