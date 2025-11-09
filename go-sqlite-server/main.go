package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

var (
	db              *sql.DB
	insertQueue     = make(chan func(), 1000)
	cleanupInterval time.Duration
	sizeLimitBytes  int64
	dbPath          = "data.db"
	mu              sync.Mutex
)

// convert size string like "1GB", "500MB", etc. to bytes
func parseSize(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	}
	val, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return val * multiplier
}

func main() {
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if ci := os.Getenv("CLEANUP_INTERVAL"); ci != "" {
		cleanupInterval, _ = time.ParseDuration(ci)
	}
	if cleanupInterval == 0 {
		cleanupInterval = 24 * time.Hour
	}

	if sl := os.Getenv("SIZE_LIMIT"); sl != "" {
		sizeLimitBytes = parseSize(sl)
	}
	if sizeLimitBytes == 0 {
		sizeLimitBytes = 1 * 1024 * 1024 * 1024 // 1GB default
	}

	go processInsertQueue()
	go scheduleCleanup()

	http.HandleFunc("/create", handleCreate)
	http.HandleFunc("/insert", handleInsert)
	http.HandleFunc("/query", handleQuery)
	http.HandleFunc("/delete", handleDelete)

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	cols := r.URL.Query()["col"] // multiple ?col=name:type

	if table == "" || len(cols) == 0 {
		http.Error(w, "Missing table or columns", 400)
		return
	}

	colsDefs := []string{"date DATETIME DEFAULT CURRENT_TIMESTAMP"}
	for _, c := range cols {
		colsDefs = append(colsDefs, c)
	}

	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s);", table, strings.Join(colsDefs, ","))
	_, err := db.Exec(query)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// ✅ Create index on date column
	dateIndexName := fmt.Sprintf("idx_%s_date", table)
	_, err = db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(date);", dateIndexName, table))
	if err != nil {
		log.Printf("Failed to create date index: %v", err)
	}

	// ✅ Create index on the last user-specified column
	lastCol := strings.SplitN(cols[len(cols)-1], " ", 2)[0] // extract column name before ':'
	lastColIndexName := fmt.Sprintf("idx_%s_%s", table, lastCol)
	_, err = db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(%s);", lastColIndexName, table, lastCol))
	if err != nil {
		log.Printf("Failed to create last-column index: %v", err)
	}

	fmt.Fprintf(w, "Table '%s' created with columns: %v and indices on date, %s\n", table, cols, lastCol)
}

func handleInsert(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	values := r.URL.Query()["val"]

	if table == "" || len(values) == 0 {
		http.Error(w, "Missing table or values", 400)
		return
	}

	insertQueue <- func() {
		mu.Lock()
		defer mu.Unlock()

		cols, err := getTableColumns(table)
		if err != nil {
			log.Println("Insert error:", err)
			return
		}

		// Exclude 'date' column from manual insert
		colsWithoutDate := []string{}
		for _, c := range cols {
			if c != "date" {
				colsWithoutDate = append(colsWithoutDate, c)
			}
		}

		if len(values) != len(colsWithoutDate) {
			log.Println("Value count mismatch for insert")
			return
		}

		placeholders := strings.Repeat("?,", len(values))
		placeholders = placeholders[:len(placeholders)-1]
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(colsWithoutDate, ","), placeholders)

		_, err = db.Exec(query, convertToInterface(values)...)
		if err != nil {
			log.Println("Insert failed:", err)
		}
	}

	fmt.Fprint(w, "Insert queued\n")
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	col := r.URL.Query().Get("col")
	val := r.URL.Query().Get("val")

	if table == "" {
		http.Error(w, "Missing table", 400)
		return
	}

	query := fmt.Sprintf("SELECT * FROM %s", table)
	args := []interface{}{}
	if col != "" && val != "" {
		query += fmt.Sprintf(" WHERE %s = ?", col)
		args = append(args, val)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	results := []map[string]interface{}{}

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		rows.Scan(ptrs...)
		row := map[string]interface{}{}
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	table := r.URL.Query().Get("table")
	col := r.URL.Query().Get("col")
	val := r.URL.Query().Get("val")

	if table == "" || col == "" || val == "" {
		http.Error(w, "Missing table, col, or val", 400)
		return
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", table, col)
	_, err := db.Exec(query, val)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprint(w, "Deleted successfully\n")
}

func getTableColumns(table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		cols = append(cols, name)
	}
	return cols, nil
}

func processInsertQueue() {
	for job := range insertQueue {
		job()
	}
}

func scheduleCleanup() {
	for range time.Tick(5 * time.Minute) {
		mu.Lock()
		log.Println("Running cleanup...")

		tables, _ := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
		for tables.Next() {
			var t string
			tables.Scan(&t)
			q := fmt.Sprintf("DELETE FROM %s WHERE date < datetime('now', '-%d seconds')", t, int(cleanupInterval.Seconds()))
			db.Exec(q)
		}

		fileInfo, err := os.Stat(dbPath)
		if err == nil && fileInfo.Size() > sizeLimitBytes {
			log.Println("Database exceeded size limit, vacuuming...")
			db.Exec("VACUUM;")
		}

		mu.Unlock()
	}
}

func convertToInterface(vals []string) []interface{} {
	res := make([]interface{}, len(vals))
	for i, v := range vals {
		res[i] = v
	}
	return res
}
