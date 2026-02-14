package server

import (
	"encoding/json"
	"fmt"
	"go-upkeep/internal/models"
	"go-upkeep/internal/monitor"
	"go-upkeep/internal/store"
	"html/template"
	"net/http"
	"sort"
)

type ServerConfig struct {
	Port         int
	EnableStatus bool
	Title        string
	ClusterKey   string // Shared Secret for Security
}

func Start(cfg ServerConfig) {
	mux := http.NewServeMux()

	// 1. Push Heartbeat
	mux.HandleFunc("/api/push", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" { http.Error(w, "Missing token", 400); return }
		if monitor.RecordHeartbeat(token) {
			w.WriteHeader(http.StatusOK); w.Write([]byte("OK"))
		} else {
			http.Error(w, "Invalid Token", 404)
		}
	})

	// 2. Health Check (For Cluster Follower)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if cfg.ClusterKey != "" && r.Header.Get("X-Upkeep-Secret") != cfg.ClusterKey {
			http.Error(w, "Unauthorized", 401)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 3. Config Export
	mux.HandleFunc("/api/backup/export", func(w http.ResponseWriter, r *http.Request) {
		if cfg.ClusterKey == "" || r.Header.Get("X-Upkeep-Secret") != cfg.ClusterKey {
			http.Error(w, "Unauthorized: UPKEEP_CLUSTER_SECRET required", 401)
			return
		}
		data := store.Get().ExportData()
		json.NewEncoder(w).Encode(data)
	})

	// 4. Config Import
	mux.HandleFunc("/api/backup/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" { http.Error(w, "POST required", 405); return }
		if cfg.ClusterKey == "" || r.Header.Get("X-Upkeep-Secret") != cfg.ClusterKey {
			http.Error(w, "Unauthorized", 401)
			return
		}
		var data models.Backup
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := store.Get().ImportData(data); err != nil {
			http.Error(w, "Import Failed: "+err.Error(), 500)
			return
		}
		w.Write([]byte("Import Successful"))
	})

	// 5. Status Page
	if cfg.EnableStatus {
		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) { renderStatusPage(w, cfg.Title) })
		mux.HandleFunc("/status/json", func(w http.ResponseWriter, r *http.Request) {
			monitor.Mutex.RLock(); defer monitor.Mutex.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(monitor.LiveState)
		})
	}

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Port)
		fmt.Printf("HTTP Server listening on %s\n", addr)
		http.ListenAndServe(addr, mux)
	}()
}

func renderStatusPage(w http.ResponseWriter, title string) {
	monitor.Mutex.RLock()
	var sites []models.Site
	for _, s := range monitor.LiveState {
		sites = append(sites, s)
	}
	monitor.Mutex.RUnlock()
	
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].Status != sites[j].Status {
			if sites[i].Status == "DOWN" { return true }
			if sites[j].Status == "DOWN" { return false }
		}
		return sites[i].Name < sites[j].Name
	})

	const tpl = `
	<!DOCTYPE html>
	<html>
	<head>
		<title>{{.Title}}</title>
		<meta http-equiv="refresh" content="5">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<style>
			body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: #1a1b26; color: #a9b1d6; padding: 20px; margin: 0; }
			h1 { text-align: center; color: #7aa2f7; margin-bottom: 30px; }
			.container { max-width: 800px; margin: 0 auto; }
			.card { background: #24283b; padding: 20px; margin-bottom: 15px; border-radius: 8px; display: flex; align-items: center; justify-content: space-between; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
			.info { display: flex; flex-direction: column; }
			.name { font-size: 1.2em; font-weight: bold; color: #c0caf5; margin-bottom: 5px; }
			.meta { font-size: 0.85em; color: #565f89; }
			.status { font-weight: bold; padding: 6px 12px; border-radius: 6px; min-width: 60px; text-align: center; }
			.UP { background: #9ece6a; color: #1a1b26; }
			.DOWN { background: #f7768e; color: #1a1b26; }
			.PENDING { background: #e0af68; color: #1a1b26; }
			.SSLEXP { background: #e0af68; color: #1a1b26; }
		</style>
	</head>
	<body>
		<div class="container">
			<h1>{{.Title}}</h1>
			{{range .Sites}}
			<div class="card">
				<div class="info">
					<div class="name">{{.Name}}</div>
					<div class="meta">{{.Type}} | {{if eq .Type "http"}}{{.URL}}{{else}}Heartbeat Monitor{{end}}</div>
					<div class="meta" style="margin-top:4px;">Last Check: {{.LastCheck.Format "15:04:05"}}</div>
				</div>
				<div class="status {{.Status}}">{{.Status}}</div>
			</div>
			{{end}}
			<div style="text-align: center; margin-top: 40px; color: #565f89; font-size: 0.8em;">Powered by Go-Upkeep</div>
		</div>
		<script>
			setTimeout(function(){ window.location.reload(1); }, 5000);
		</script>
	</body>
	</html>`

	t, _ := template.New("status").Parse(tpl)
	data := struct { Title string; Sites []models.Site }{Title: title, Sites: sites}
	t.Execute(w, data)
}