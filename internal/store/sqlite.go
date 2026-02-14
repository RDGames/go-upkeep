package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"go-upkeep/internal/models"
	
	_ "github.com/mattn/go-sqlite3"
)

type SQLiteStore struct {
	DBPath string
	db     *sql.DB
}

func (s *SQLiteStore) Init() error {
	var err error
	s.db, err = sql.Open("sqlite3", s.DBPath)
	if err != nil { return err }

	createTables := `
	CREATE TABLE IF NOT EXISTS alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		type TEXT,
		settings TEXT
	);
	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT DEFAULT 'New Monitor',
		url TEXT,
		type TEXT DEFAULT 'http',
		token TEXT,
		interval INTEGER,
		alert_id INTEGER,
		check_ssl BOOLEAN DEFAULT 0,
		threshold INTEGER DEFAULT 7,
		max_retries INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		public_key TEXT NOT NULL,
		role TEXT DEFAULT 'user'
	);`
	_, err = s.db.Exec(createTables)
	return err
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *SQLiteStore) GetSites() []models.Site {
	rows, err := s.db.Query("SELECT id, COALESCE(name, url), url, COALESCE(type, 'http'), COALESCE(token, ''), interval, alert_id, check_ssl, threshold, max_retries FROM sites")
	if err != nil { return []models.Site{} }
	defer rows.Close()
	var sites []models.Site
	for rows.Next() {
		var st models.Site
		rows.Scan(&st.ID, &st.Name, &st.URL, &st.Type, &st.Token, &st.Interval, &st.AlertID, &st.CheckSSL, &st.ExpiryThreshold, &st.MaxRetries)
		sites = append(sites, st)
	}
	return sites
}
func (s *SQLiteStore) AddSite(name, url, sType string, interval, alertID int, checkSSL bool, threshold, retries int) {
	token := ""
	if sType == "push" { token = generateToken() }
	s.db.Exec("INSERT INTO sites (name, url, type, token, interval, alert_id, check_ssl, threshold, max_retries) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", name, url, sType, token, interval, alertID, checkSSL, threshold, retries)
}
func (s *SQLiteStore) UpdateSite(id int, name, url, sType string, interval, alertID int, checkSSL bool, threshold, retries int) {
	var existingToken string
	s.db.QueryRow("SELECT token FROM sites WHERE id=?", id).Scan(&existingToken)
	if sType == "push" && existingToken == "" { existingToken = generateToken() }
	s.db.Exec("UPDATE sites SET name=?, url=?, type=?, token=?, interval=?, alert_id=?, check_ssl=?, threshold=?, max_retries=? WHERE id=?", name, url, sType, existingToken, interval, alertID, checkSSL, threshold, retries, id)
}
func (s *SQLiteStore) DeleteSite(id int) {
	s.db.Exec("DELETE FROM sites WHERE id=?", id)
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM sites").Scan(&count)
	if count == 0 { s.db.Exec("DELETE FROM sqlite_sequence WHERE name='sites'") }
}
func (s *SQLiteStore) GetAllAlerts() []models.AlertConfig {
	rows, err := s.db.Query("SELECT id, name, type, settings FROM alerts")
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
func (s *SQLiteStore) GetAlert(id int) (models.AlertConfig, bool) {
	var a models.AlertConfig; var settingsJSON string
	err := s.db.QueryRow("SELECT id, name, type, settings FROM alerts WHERE id = ?", id).Scan(&a.ID, &a.Name, &a.Type, &settingsJSON)
	if err != nil { return a, false }
	json.Unmarshal([]byte(settingsJSON), &a.Settings)
	return a, true
}
func (s *SQLiteStore) AddAlert(name, aType string, settings map[string]string) {
	jsonBytes, _ := json.Marshal(settings)
	s.db.Exec("INSERT INTO alerts (name, type, settings) VALUES (?, ?, ?)", name, aType, string(jsonBytes))
}
func (s *SQLiteStore) UpdateAlert(id int, name, aType string, settings map[string]string) {
	jsonBytes, _ := json.Marshal(settings)
	s.db.Exec("UPDATE alerts SET name=?, type=?, settings=? WHERE id=?", name, aType, string(jsonBytes), id)
}
func (s *SQLiteStore) DeleteAlert(id int) {
	s.db.Exec("DELETE FROM alerts WHERE id=?", id)
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM alerts").Scan(&count)
	if count == 0 { s.db.Exec("DELETE FROM sqlite_sequence WHERE name='alerts'") }
}
func (s *SQLiteStore) GetAllUsers() []models.User {
	rows, err := s.db.Query("SELECT id, username, public_key, role FROM users")
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
func (s *SQLiteStore) AddUser(username, publicKey, role string) error {
	_, err := s.db.Exec("INSERT INTO users (username, public_key, role) VALUES (?, ?, ?)", username, publicKey, role)
	return err
}
func (s *SQLiteStore) DeleteUser(id int) error {
	_, err := s.db.Exec("DELETE FROM users WHERE id=?", id)
	return err
}

// --- PHASE 5 ---

func (s *SQLiteStore) ExportData() models.Backup {
	return models.Backup{
		Sites:  s.GetSites(),
		Alerts: s.GetAllAlerts(),
		Users:  s.GetAllUsers(),
	}
}

func (s *SQLiteStore) ImportData(data models.Backup) error {
	tx, err := s.db.Begin()
	if err != nil { return err }

	// Wipe Existing
	tx.Exec("DELETE FROM sites"); tx.Exec("DELETE FROM sqlite_sequence WHERE name='sites'")
	tx.Exec("DELETE FROM alerts"); tx.Exec("DELETE FROM sqlite_sequence WHERE name='alerts'")
	tx.Exec("DELETE FROM users"); tx.Exec("DELETE FROM sqlite_sequence WHERE name='users'")

	// Insert New
	for _, u := range data.Users {
		tx.Exec("INSERT INTO users (username, public_key, role) VALUES (?, ?, ?)", u.Username, u.PublicKey, u.Role)
	}
	for _, a := range data.Alerts {
		jsonBytes, _ := json.Marshal(a.Settings)
		tx.Exec("INSERT INTO alerts (id, name, type, settings) VALUES (?, ?, ?, ?)", a.ID, a.Name, a.Type, string(jsonBytes))
	}
	for _, st := range data.Sites {
		tx.Exec("INSERT INTO sites (id, name, url, type, token, interval, alert_id, check_ssl, threshold, max_retries) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			st.ID, st.Name, st.URL, st.Type, st.Token, st.Interval, st.AlertID, st.CheckSSL, st.ExpiryThreshold, st.MaxRetries)
	}

	return tx.Commit()
}