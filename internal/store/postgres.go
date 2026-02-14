package store

import (
	"database/sql"
	"encoding/json"
	"go-upkeep/internal/models"
	
	_ "github.com/lib/pq"
)

type PostgresStore struct {
	ConnStr string
	db      *sql.DB
}

func (p *PostgresStore) Init() error {
	var err error
	p.db, err = sql.Open("postgres", p.ConnStr)
	if err != nil { return err }

	queries := []string{
		`CREATE TABLE IF NOT EXISTS alerts (
			id SERIAL PRIMARY KEY,
			name TEXT,
			type TEXT,
			settings TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS sites (
			id SERIAL PRIMARY KEY,
			name TEXT DEFAULT 'New Monitor',
			url TEXT,
			type TEXT DEFAULT 'http',
			token TEXT,
			interval INTEGER,
			alert_id INTEGER,
			check_ssl BOOLEAN DEFAULT FALSE,
			threshold INTEGER DEFAULT 7,
			max_retries INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username TEXT NOT NULL,
			public_key TEXT NOT NULL,
			role TEXT DEFAULT 'user'
		);`,
	}
	for _, q := range queries {
		if _, err := p.db.Exec(q); err != nil { return err }
	}
	return nil
}

// ... [CRUD Methods are identical to Phase 4, keeping them concise here] ...
func (p *PostgresStore) GetSites() []models.Site {
	rows, err := p.db.Query("SELECT id, COALESCE(name, url), url, COALESCE(type, 'http'), COALESCE(token, ''), interval, alert_id, check_ssl, threshold, max_retries FROM sites")
	if err != nil { return []models.Site{} }
	defer rows.Close()
	var sites []models.Site
	for rows.Next() {
		var s models.Site
		rows.Scan(&s.ID, &s.Name, &s.URL, &s.Type, &s.Token, &s.Interval, &s.AlertID, &s.CheckSSL, &s.ExpiryThreshold, &s.MaxRetries)
		sites = append(sites, s)
	}
	return sites
}
func (p *PostgresStore) AddSite(name, url, sType string, interval, alertID int, checkSSL bool, threshold, retries int) {
	token := ""
	if sType == "push" { token = generateToken() }
	p.db.Exec("INSERT INTO sites (name, url, type, token, interval, alert_id, check_ssl, threshold, max_retries) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)", name, url, sType, token, interval, alertID, checkSSL, threshold, retries)
}
func (p *PostgresStore) UpdateSite(id int, name, url, sType string, interval, alertID int, checkSSL bool, threshold, retries int) {
	var existingToken string
	p.db.QueryRow("SELECT token FROM sites WHERE id=$1", id).Scan(&existingToken)
	if sType == "push" && existingToken == "" { existingToken = generateToken() }
	p.db.Exec("UPDATE sites SET name=$1, url=$2, type=$3, token=$4, interval=$5, alert_id=$6, check_ssl=$7, threshold=$8, max_retries=$9 WHERE id=$10", name, url, sType, existingToken, interval, alertID, checkSSL, threshold, retries, id)
}
func (p *PostgresStore) DeleteSite(id int) { p.db.Exec("DELETE FROM sites WHERE id=$1", id) }
func (p *PostgresStore) GetAllAlerts() []models.AlertConfig {
	rows, err := p.db.Query("SELECT id, name, type, settings FROM alerts")
	if err != nil { return []models.AlertConfig{} }
	defer rows.Close()
	var alerts []models.AlertConfig
	for rows.Next() {
		var a models.AlertConfig; var settingsJSON string
		rows.Scan(&a.ID, &a.Name, &a.Type, &settingsJSON)
		json.Unmarshal([]byte(settingsJSON), &a.Settings)
		alerts = append(alerts, a)
	}
	return alerts
}
func (p *PostgresStore) GetAlert(id int) (models.AlertConfig, bool) {
	var a models.AlertConfig; var settingsJSON string
	err := p.db.QueryRow("SELECT id, name, type, settings FROM alerts WHERE id = $1", id).Scan(&a.ID, &a.Name, &a.Type, &settingsJSON)
	if err != nil { return a, false }
	json.Unmarshal([]byte(settingsJSON), &a.Settings)
	return a, true
}
func (p *PostgresStore) AddAlert(name, aType string, settings map[string]string) {
	jsonBytes, _ := json.Marshal(settings)
	p.db.Exec("INSERT INTO alerts (name, type, settings) VALUES ($1, $2, $3)", name, aType, string(jsonBytes))
}
func (p *PostgresStore) UpdateAlert(id int, name, aType string, settings map[string]string) {
	jsonBytes, _ := json.Marshal(settings)
	p.db.Exec("UPDATE alerts SET name=$1, type=$2, settings=$3 WHERE id=$4", name, aType, string(jsonBytes), id)
}
func (p *PostgresStore) DeleteAlert(id int) { p.db.Exec("DELETE FROM alerts WHERE id=$1", id) }
func (p *PostgresStore) GetAllUsers() []models.User {
	rows, err := p.db.Query("SELECT id, username, public_key, role FROM users")
	if err != nil { return []models.User{} }
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		rows.Scan(&u.ID, &u.Username, &u.PublicKey, &u.Role)
		users = append(users, u)
	}
	return users
}
func (p *PostgresStore) AddUser(username, publicKey, role string) error {
	_, err := p.db.Exec("INSERT INTO users (username, public_key, role) VALUES ($1, $2, $3)", username, publicKey, role)
	return err
}
func (p *PostgresStore) DeleteUser(id int) error {
	_, err := p.db.Exec("DELETE FROM users WHERE id=$1", id)
	return err
}

// --- PHASE 5 ---

func (p *PostgresStore) ExportData() models.Backup {
	return models.Backup{
		Sites:  p.GetSites(),
		Alerts: p.GetAllAlerts(),
		Users:  p.GetAllUsers(),
	}
}

func (p *PostgresStore) ImportData(data models.Backup) error {
	tx, err := p.db.Begin()
	if err != nil { return err }

	tx.Exec("TRUNCATE TABLE sites RESTART IDENTITY CASCADE")
	tx.Exec("TRUNCATE TABLE alerts RESTART IDENTITY CASCADE")
	tx.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE")

	for _, u := range data.Users {
		tx.Exec("INSERT INTO users (username, public_key, role) VALUES ($1, $2, $3)", u.Username, u.PublicKey, u.Role)
	}
	for _, a := range data.Alerts {
		jsonBytes, _ := json.Marshal(a.Settings)
		tx.Exec("INSERT INTO alerts (id, name, type, settings) VALUES ($1, $2, $3, $4)", a.ID, a.Name, a.Type, string(jsonBytes))
	}
	for _, st := range data.Sites {
		tx.Exec("INSERT INTO sites (id, name, url, type, token, interval, alert_id, check_ssl, threshold, max_retries) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)",
			st.ID, st.Name, st.URL, st.Type, st.Token, st.Interval, st.AlertID, st.CheckSSL, st.ExpiryThreshold, st.MaxRetries)
	}
	
	tx.Exec("SELECT setval('sites_id_seq', (SELECT MAX(id) FROM sites))")
	tx.Exec("SELECT setval('alerts_id_seq', (SELECT MAX(id) FROM alerts))")
	tx.Exec("SELECT setval('users_id_seq', (SELECT MAX(id) FROM users))")

	return tx.Commit()
}