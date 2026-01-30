package db

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"go-upkeep/internal/models"
	"log"
)

var DB *sql.DB

func Init(dbPath string) {
	var err error
	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	createTables := `
	CREATE TABLE IF NOT EXISTS alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		type TEXT,
		webhook_url TEXT
	);
	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT,
		interval INTEGER,
		alert_id INTEGER,
		check_ssl BOOLEAN DEFAULT 0,
		threshold INTEGER DEFAULT 7
	);
	`
	_, err = DB.Exec(createTables)
	if err != nil {
		log.Fatal(err)
	}
}

func GetSites() []models.Site {
	rows, _ := DB.Query("SELECT id, url, interval, alert_id, check_ssl, threshold FROM sites")
	defer rows.Close()
	var sites []models.Site
	for rows.Next() {
		var s models.Site
		rows.Scan(&s.ID, &s.URL, &s.Interval, &s.AlertID, &s.CheckSSL, &s.ExpiryThreshold)
		sites = append(sites, s)
	}
	return sites
}

func AddSite(url string, interval, alertID int, checkSSL bool, threshold int) {
	DB.Exec("INSERT INTO sites (url, interval, alert_id, check_ssl, threshold) VALUES (?, ?, ?, ?, ?)", 
		url, interval, alertID, checkSSL, threshold)
}

func UpdateSite(id int, url string, interval, alertID int, checkSSL bool, threshold int) {
	DB.Exec("UPDATE sites SET url=?, interval=?, alert_id=?, check_ssl=?, threshold=? WHERE id=?", 
		url, interval, alertID, checkSSL, threshold, id)
}

func DeleteSite(id int) {
	DB.Exec("DELETE FROM sites WHERE id=?", id)
	
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM sites").Scan(&count)
	if count == 0 {
		DB.Exec("DELETE FROM sqlite_sequence WHERE name='sites'")
	}
}

func GetAllAlerts() []models.AlertConfig {
	rows, _ := DB.Query("SELECT id, name, type, webhook_url FROM alerts")
	defer rows.Close()
	var alerts []models.AlertConfig
	for rows.Next() {
		var a models.AlertConfig
		rows.Scan(&a.ID, &a.Name, &a.Type, &a.WebhookURL)
		alerts = append(alerts, a)
	}
	return alerts
}

func GetAlert(id int) (models.AlertConfig, bool) {
	var a models.AlertConfig
	err := DB.QueryRow("SELECT id, name, type, webhook_url FROM alerts WHERE id = ?", id).Scan(&a.ID, &a.Name, &a.Type, &a.WebhookURL)
	if err != nil {
		return a, false
	}
	return a, true
}

func AddAlert(name, aType, url string) {
	DB.Exec("INSERT INTO alerts (name, type, webhook_url) VALUES (?, ?, ?)", name, aType, url)
}

func UpdateAlert(id int, name, aType, url string) {
	DB.Exec("UPDATE alerts SET name=?, type=?, webhook_url=? WHERE id=?", name, aType, url, id)
}

func DeleteAlert(id int) {
	DB.Exec("DELETE FROM alerts WHERE id=?", id)

	var count int
	DB.QueryRow("SELECT COUNT(*) FROM alerts").Scan(&count)
	if count == 0 {
		DB.Exec("DELETE FROM sqlite_sequence WHERE name='alerts'")
	}
}