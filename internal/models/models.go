package models

import "time"

type Site struct {
	ID              int
	Name            string
	URL             string
	Type            string // "http" or "push"
	Token           string // Secure Token
	Interval        int
	AlertID         int
	CheckSSL        bool
	ExpiryThreshold int
	
	MaxRetries      int
	FailureCount    int

	Status          string
	StatusCode      int
	Latency         time.Duration
	CertExpiry      time.Time
	HasSSL          bool
	LastCheck       time.Time
	SentSSLWarning  bool 
}

type AlertConfig struct {
	ID       int
	Name     string
	Type     string
	Settings map[string]string
}

type User struct {
	ID        int
	Username  string
	PublicKey string
	Role      string 
}

// Phase 5: Backup Structure
type Backup struct {
	Sites  []Site        `json:"sites"`
	Alerts []AlertConfig `json:"alerts"`
	Users  []User        `json:"users"`
}