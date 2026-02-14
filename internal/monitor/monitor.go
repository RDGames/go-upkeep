package monitor

import (
	"crypto/tls"
	"fmt"
	"go-upkeep/internal/alert"
	"go-upkeep/internal/models"
	"go-upkeep/internal/store"
	"net/http"
	"sync"
	"time"
)

// --- LOGGING ---
var (
	LogStore []string
	LogMutex sync.RWMutex
)

func AddLog(msg string) {
	LogMutex.Lock(); defer LogMutex.Unlock()
	ts := time.Now().Format("15:04:05"); entry := fmt.Sprintf("[%s] %s", ts, msg)
	LogStore = append([]string{entry}, LogStore...)
	if len(LogStore) > 100 { LogStore = LogStore[:100] }
}

func GetLogs() []string {
	LogMutex.RLock(); defer LogMutex.RUnlock(); logs := make([]string, len(LogStore)); copy(logs, LogStore); return logs
}

// --- ENGINE ---

var (
	LiveState = make(map[int]models.Site)
	Mutex     sync.RWMutex
	
	// Global Switch for HA
	isActive     = true
	activeMutex  sync.RWMutex
)

func SetEngineActive(active bool) {
	activeMutex.Lock()
	defer activeMutex.Unlock()
	if isActive != active {
		isActive = active
		status := "RESUMED (Active)"
		if !active { status = "PAUSED (Passive)" }
		AddLog(fmt.Sprintf("Engine %s", status))
	}
}

func IsEngineActive() bool {
	activeMutex.RLock()
	defer activeMutex.RUnlock()
	return isActive
}

func RecordHeartbeat(token string) bool {
	if !IsEngineActive() { return false } // Only Leader accepts Push
	
	Mutex.Lock(); defer Mutex.Unlock()
	var targetID int = -1
	for id, s := range LiveState {
		if s.Type == "push" && s.Token == token { targetID = id; break }
	}
	if targetID == -1 { return false }

	site := LiveState[targetID]
	site.LastCheck = time.Now()
	wasDown := site.Status == "DOWN"
	site.Status = "UP"; site.FailureCount = 0; site.Latency = 0 
	LiveState[targetID] = site
	
	if wasDown {
		AddLog(fmt.Sprintf("Push Monitor '%s' recovered", site.Name))
		triggerAlert(site.AlertID, "✅ RECOVERY", fmt.Sprintf("Push Monitor '%s' is receiving heartbeats.", site.Name))
	}
	return true
}

func StartEngine() {
	go func() {
		for {
			s_instance := store.Get()
			if s_instance == nil { time.Sleep(1 * time.Second); continue }
			
			sites := s_instance.GetSites()
			for _, s := range sites {
				Mutex.RLock(); _, exists := LiveState[s.ID]; Mutex.RUnlock()
				if !exists {
					Mutex.Lock()
					s.Status = "PENDING"
					if s.Type == "push" { s.LastCheck = time.Now() }
					LiveState[s.ID] = s
					Mutex.Unlock()
					go monitorRoutine(s.ID)
				}
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

func UpdateSiteConfig(id int, name, url, sType string, interval, alertID int, checkSSL bool, threshold, retries int) {
	Mutex.Lock(); defer Mutex.Unlock()
	if s, ok := LiveState[id]; ok {
		s.Name = name; s.URL = url; s.Type = sType; s.Interval = interval
		s.AlertID = alertID; s.CheckSSL = checkSSL; s.ExpiryThreshold = threshold; s.MaxRetries = retries
		LiveState[id] = s
	}
}

func RemoveSite(id int) { Mutex.Lock(); delete(LiveState, id); Mutex.Unlock() }

func monitorRoutine(id int) {
	checkByID(id)
	for {
		// If paused, just sleep loop to keep goroutine alive but idle
		if !IsEngineActive() {
			time.Sleep(5 * time.Second)
			continue
		}

		Mutex.RLock(); site, exists := LiveState[id]; Mutex.RUnlock()
		if !exists { return }
		
		interval := site.Interval; if interval < 5 { interval = 5 }
		time.Sleep(time.Duration(interval) * time.Second)
		checkByID(id)
	}
}

func checkByID(id int) {
	if !IsEngineActive() { return }

	Mutex.RLock(); site, exists := LiveState[id]; Mutex.RUnlock()
	if !exists { return }
	if site.Type == "http" { checkHTTP(site) } else { checkPush(site) }
}

func checkPush(site models.Site) {
	deadline := site.LastCheck.Add(time.Duration(site.Interval) * time.Second).Add(5 * time.Second)
	if time.Now().After(deadline) {
		handleStatusChange(site, "DOWN", 0, 0)
	} else {
		if site.Status != "UP" { handleStatusChange(site, "UP", 200, 0) }
	}
}

func checkHTTP(site models.Site) {
	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get(site.URL)
	latency := time.Since(start)

	rawStatus := "UP"; rawCode := 0; var certExpiry time.Time; hasSSL := false

	if err != nil { rawStatus = "DOWN"
	} else {
		defer resp.Body.Close(); rawCode = resp.StatusCode
		if resp.StatusCode >= 400 { rawStatus = "DOWN" }
		if site.CheckSSL && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			hasSSL = true; cert := resp.TLS.PeerCertificates[0]; certExpiry = cert.NotAfter
			if time.Now().After(cert.NotAfter) { rawStatus = "SSL EXP" }
		}
	}
	updatedSite := site
	updatedSite.HasSSL = hasSSL; updatedSite.CertExpiry = certExpiry
	updatedSite.Latency = latency; updatedSite.LastCheck = time.Now()
	handleStatusChange(updatedSite, rawStatus, rawCode, latency)
}

func handleStatusChange(site models.Site, rawStatus string, code int, latency time.Duration) {
	// Double check we are still leader before alerting
	if !IsEngineActive() { return }

	newState := site
	newState.StatusCode = code

	if site.Status == "UP" && rawStatus != "UP" {
		newState.FailureCount++
		if newState.FailureCount > site.MaxRetries {
			newState.Status = rawStatus; newState.FailureCount = site.MaxRetries + 1
			AddLog(fmt.Sprintf("Monitor '%s' confirmed DOWN", site.Name))
		} else {
			AddLog(fmt.Sprintf("Monitor '%s' failed check %d/%d", site.Name, newState.FailureCount, site.MaxRetries))
		}
	} else if rawStatus == "UP" {
		newState.FailureCount = 0; newState.Status = "UP"
	} else {
		newState.Status = rawStatus; newState.FailureCount = site.MaxRetries + 1 
	}
	
	if site.Type == "http" && site.CheckSSL && site.HasSSL {
		daysLeft := int(time.Until(site.CertExpiry).Hours() / 24)
		if daysLeft <= site.ExpiryThreshold && !site.SentSSLWarning && rawStatus != "SSL EXP" {
			triggerAlert(site.AlertID, "SSL WARNING", fmt.Sprintf("SSL for '%s' expires in %d days", site.Name, daysLeft))
			newState.SentSSLWarning = true
		} else if daysLeft > site.ExpiryThreshold { newState.SentSSLWarning = false }
	}

	Mutex.Lock(); if _, ok := LiveState[site.ID]; ok { LiveState[site.ID] = newState }; Mutex.Unlock()

	isBroken := func(s string) bool { return s == "DOWN" || s == "SSL EXP" }
	if !isBroken(site.Status) && isBroken(newState.Status) && newState.Status != "PENDING" {
		msg := fmt.Sprintf("Monitor '%s' is DOWN (%s)", site.Name, rawStatus)
		if site.Type == "push" { msg = fmt.Sprintf("Push Monitor '%s' missed heartbeat.", site.Name) }
		triggerAlert(site.AlertID, "🚨 ALERT", msg)
	}
	if isBroken(site.Status) && newState.Status == "UP" {
		triggerAlert(site.AlertID, "✅ RECOVERY", fmt.Sprintf("Monitor '%s' is UP", site.Name))
	}
}

func triggerAlert(alertID int, title, message string) {
	s_instance := store.Get(); if s_instance == nil { return }
	cfg, ok := s_instance.GetAlert(alertID); if !ok { return }
	provider := alert.GetProvider(cfg)
	if provider != nil { go func() { provider.Send(title, message) }() }
}