package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go-upkeep/internal/models"
	"net/http"
	"net/smtp"
)

type Provider interface {
	Send(title, message string) error
}

func GetProvider(cfg models.AlertConfig) Provider {
	switch cfg.Type {
	case "discord":
		return &DiscordProvider{URL: cfg.Settings["url"]}
	case "slack":
		return &SlackProvider{URL: cfg.Settings["url"]}
	case "webhook":
		// Generic Webhook
		return &WebhookProvider{URL: cfg.Settings["url"]}
	case "email":
		port := "25"
		if p, ok := cfg.Settings["port"]; ok { port = p }
		return &EmailProvider{
			Host: cfg.Settings["host"],
			Port: port,
			User: cfg.Settings["user"],
			Pass: cfg.Settings["pass"],
			To:   cfg.Settings["to"],
			From: cfg.Settings["from"],
		}
	default:
		return nil
	}
}

// --- DISCORD ---
type DiscordProvider struct{ URL string }
func (d *DiscordProvider) Send(title, message string) error {
	payload := map[string]string{"content": fmt.Sprintf("**%s**\n%s", title, message)}
	jsonValue, _ := json.Marshal(payload)
	_, err := http.Post(d.URL, "application/json", bytes.NewBuffer(jsonValue))
	return err
}

// --- SLACK ---
type SlackProvider struct{ URL string }
func (s *SlackProvider) Send(title, message string) error {
	payload := map[string]string{"text": fmt.Sprintf("*%s*\n%s", title, message)}
	jsonValue, _ := json.Marshal(payload)
	_, err := http.Post(s.URL, "application/json", bytes.NewBuffer(jsonValue))
	return err
}

// --- GENERIC WEBHOOK ---
type WebhookProvider struct{ URL string }
func (w *WebhookProvider) Send(title, message string) error {
	// Sends a standard JSON payload
	payload := map[string]string{
		"title":   title,
		"message": message,
		"status":  "alert",
	}
	jsonValue, _ := json.Marshal(payload)
	_, err := http.Post(w.URL, "application/json", bytes.NewBuffer(jsonValue))
	return err
}

// --- EMAIL ---
type EmailProvider struct {
	Host, Port, User, Pass, To, From string
}
func (e *EmailProvider) Send(title, message string) error {
	auth := smtp.PlainAuth("", e.User, e.Pass, e.Host)
	msg := []byte("To: " + e.To + "\r\n" +
		"Subject: Go-Upkeep: " + title + "\r\n" +
		"\r\n" +
		message + "\r\n")
	return smtp.SendMail(e.Host+":"+e.Port, auth, e.From, []string{e.To}, msg)
}