package monitor

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"go-upkeep/internal/db"
	"go-upkeep/internal/models"
	"net/http"
	"sync"
	"time"
)

var (
	LogStore []string
	LogMutex sync.RWMutex
)

func AddLog(msg string) {
	LogMutex.Lock()
	defer LogMutex.Unlock()
	ts := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s", ts, msg)
	
	LogStore = append([]string{entry}, LogStore...)
	if len(LogStore) > 100 {
		LogStore = LogStore[:100]
	}
}

func GetLogs() []string {
	LogMutex.RLock()
	defer LogMutex.RUnlock()
	logs := make([]string, len(LogStore))
	copy(logs, LogStore)
	return logs
}

var (
	LiveState = make(map[int]models.Site)
	Mutex     sync.RWMutex
)

func StartEngine() {
	go func() {
		for {
			sites := db.GetSites()
			for _, s := range sites {
				Mutex.RLock()
				_, exists := LiveState[s.ID]
				Mutex.RUnlock()

				if !exists {
					Mutex.Lock()
					s.Status = "PENDING"
					LiveState[s.ID] = s
					Mutex.Unlock()
					go monitorRoutine(s)
				}
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

func UpdateSiteConfig(id int, url string, interval, alertID int, checkSSL bool, threshold int) {
	Mutex.Lock()
	defer Mutex.Unlock()
	if s, ok := LiveState[id]; ok {
		s.URL = url
		s.Interval = interval
		s.AlertID = alertID
		s.CheckSSL = checkSSL
		s.ExpiryThreshold = threshold
		LiveState[id] = s
	}
}

func RemoveSite(id int) {
	Mutex.Lock()
	delete(LiveState, id)
	Mutex.Unlock()
}

func monitorRoutine(initialData models.Site) {
	ticker := time.NewTicker(time.Duration(initialData.Interval) * time.Second)
	check(initialData.ID)
	for range ticker.C {
		Mutex.RLock()
		_, exists := LiveState[initialData.ID]
		Mutex.RUnlock()
		if !exists {
			ticker.Stop()
			return
		}
		check(initialData.ID)
	}
}

func check(siteID int) {
	Mutex.RLock()
	site, exists := LiveState[siteID]
	Mutex.RUnlock()
	if !exists { return }

	start := time.Now()
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	resp, err := client.Get(site.URL)
	latency := time.Since(start)

	newState := site
	newState.LastCheck = time.Now()
	newState.Latency = latency
	previousStatus := site.Status

	if err != nil {
		newState.Status = "DOWN"
		newState.StatusCode = 0
	} else {
		defer resp.Body.Close()
		newState.StatusCode = resp.StatusCode
		if resp.StatusCode >= 400 {
			newState.Status = "DOWN"
		} else {
			newState.Status = "UP"
		}

		if site.CheckSSL && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			newState.HasSSL = true
			cert := resp.TLS.PeerCertificates[0]
			newState.CertExpiry = cert.NotAfter
			daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)

			if time.Now().After(cert.NotAfter) {
				newState.Status = "SSL EXP"
			} else if daysLeft <= site.ExpiryThreshold {
				if !site.SentSSLWarning {
					msg := fmt.Sprintf("SSL WARNING: %s expires in %d days", site.URL, daysLeft)
					AddLog(msg)
					sendRawAlert(site.AlertID, msg)
					newState.SentSSLWarning = true
				}
			} else {
				newState.SentSSLWarning = false
			}
		}
	}

	Mutex.Lock()
	if _, ok := LiveState[siteID]; ok {
		LiveState[siteID] = newState
	}
	Mutex.Unlock()

	isBroken := func(s string) bool { return s == "DOWN" || s == "SSL EXP" }

	if !isBroken(previousStatus) && isBroken(newState.Status) {
		sendAlert(site.AlertID, site.URL, newState.Status, newState.StatusCode)
	}
	if isBroken(previousStatus) && !isBroken(newState.Status) && previousStatus != "PENDING" {
		sendAlert(site.AlertID, site.URL, "RECOVERED", newState.StatusCode)
	}
}

func sendAlert(alertID int, siteURL, status string, code int) {
	var msg string
	if status == "SSL EXP" {
		msg = fmt.Sprintf("**SSL EXPIRED**: The certificate for %s has expired! (Code: %d)", siteURL, code)
	} else if status == "RECOVERED" {
		msg = fmt.Sprintf("**RECOVERY**: %s is back UP (Code: %d)", siteURL, code)
	} else {
		msg = fmt.Sprintf("**ALERT**: %s is %s (Code: %d)", siteURL, status, code)
	}

	AddLog(fmt.Sprintf("Alert Sent: %s is %s", siteURL, status))
	sendRawAlert(alertID, msg)
}

func sendRawAlert(alertID int, message string) {
	if alertID == 0 { return }
	cfg, ok := db.GetAlert(alertID)
	if !ok { return }

	if cfg.Type == "discord" {
		payload := map[string]string{"content": message}
		jsonValue, _ := json.Marshal(payload)
		http.Post(cfg.WebhookURL, "application/json", bytes.NewBuffer(jsonValue))
	}
}