package models

import "time"

type Site struct {
	ID              int
	URL             string
	Interval        int
	AlertID         int

	CheckSSL        bool
	ExpiryThreshold int

	Status          string
	StatusCode      int
	Latency         time.Duration
	CertExpiry      time.Time
	HasSSL          bool
	LastCheck       time.Time

	SentSSLWarning  bool 
}

type AlertConfig struct {
	ID         int
	Name       string
	Type       string
	WebhookURL string
}