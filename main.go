package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

const (
	dbHost     = "localhost"
	dbPort     = "5432"
	dbUser     = "validator"
	dbPassword = "val1dat0r"
	dbName     = "project-sem-1"
	tableName  = "prices"
)

var db *sql.DB

type PriceRecord struct {
	ID         int
	Name       string
	Category   string
	Price      float64
	CreateDate string
}

type UploadResponse struct {
	TotalItems      int     `json:"total_items"`
	TotalCategories int     `json:"total_categories"`
	TotalPrice      float64 `json:"total_price"`
}

func initDB() error {
	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	var err error
	db, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	createTableQuery := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER,
			name VARCHAR(255),
			category VARCHAR(255),
			price DECIMAL(10, 2),
			create_date DATE
		)`, tableName)

	_, err = db.Exec(createTableQuery)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	log.Println("Database connection established and table ready")
	return nil
}

func handlePostPrices(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	if !strings.HasSuffix(header.Filename, ".zip") {
		http.Error(w, "File must be a zip archive", http.StatusBadRequest)
		return
	}

	zipData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		http.Error(w, "Failed to open zip: "+err.Error(), http.StatusBadRequest)
		return
	}

	var csvData []byte
	found := false
	for _, f := range zipReader.File {
		if f.Name == "data.csv" {
			rc, err := f.Open()
			if err != nil {
				http.Error(w, "Failed to open data.csv: "+err.Error(), http.StatusInternalServerError)
				return
			}
			csvData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				http.Error(w, "Failed to read data.csv: "+err.Error(), http.StatusInternalServerError)
				return
			}
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "data.csv not found in zip archive", http.StatusBadRequest)
		return
	}

	csvReader := csv.NewReader(strings.NewReader(string(csvData)))
	records, err := csvReader.ReadAll()
	if err != nil {
		http.Error(w, "Failed to parse CSV: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(records) < 2 {
		http.Error(w, "CSV file must contain at least a header and one data row", http.StatusBadRequest)
		return
	}

	insertQuery := fmt.Sprintf("INSERT INTO %s (id, name, category, price, create_date) VALUES ($1, $2, $3, $4, $5)", tableName)
	stmt, err := db.Prepare(insertQuery)
	if err != nil {
		http.Error(w, "Failed to prepare statement: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	totalItems := 0
	categoriesMap := make(map[string]bool)
	var totalPrice float64

	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 5 {
			continue
		}

		id, err := strconv.Atoi(record[0])
		if err != nil {
			continue
		}

		name := record[1]
		category := record[2]
		price, err := strconv.ParseFloat(record[3], 64)
		if err != nil {
			continue
		}

		createDate := record[4]

		_, err = stmt.Exec(id, name, category, price, createDate)
		if err != nil {
			log.Printf("Failed to insert record: %v", err)
			continue
		}

		totalItems++
		categoriesMap[category] = true
		totalPrice += price
	}

	response := UploadResponse{
		TotalItems:      totalItems,
		TotalCategories: len(categoriesMap),
		TotalPrice:      totalPrice,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleGetPrices(w http.ResponseWriter, r *http.Request) {
	query := fmt.Sprintf("SELECT id, name, category, price, create_date FROM %s ORDER BY id", tableName)
	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, "Failed to query database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var records []PriceRecord
	for rows.Next() {
		var rec PriceRecord
		err := rows.Scan(&rec.ID, &rec.Name, &rec.Category, &rec.Price, &rec.CreateDate)
		if err != nil {
			http.Error(w, "Failed to scan record: "+err.Error(), http.StatusInternalServerError)
			return
		}
		records = append(records, rec)
	}

	if err = rows.Err(); err != nil {
		http.Error(w, "Error iterating rows: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tmpCSV, err := os.CreateTemp("", "data*.csv")
	if err != nil {
		http.Error(w, "Failed to create temp file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpCSV.Name())
	defer tmpCSV.Close()

	csvWriter := csv.NewWriter(tmpCSV)
	csvWriter.Write([]string{"id", "name", "category", "price", "create_date"})

	for _, rec := range records {
		row := []string{
			strconv.Itoa(rec.ID),
			rec.Name,
			rec.Category,
			strconv.FormatFloat(rec.Price, 'f', 2, 64),
			rec.CreateDate,
		}
		csvWriter.Write(row)
	}
	csvWriter.Flush()

	if err = csvWriter.Error(); err != nil {
		http.Error(w, "Failed to write CSV: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tmpCSV.Seek(0, 0)

	tmpZip, err := os.CreateTemp("", "data*.zip")
	if err != nil {
		http.Error(w, "Failed to create temp zip: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpZip.Name())

	zipWriter := zip.NewWriter(tmpZip)
	csvFileInZip, err := zipWriter.Create("data.csv")
	if err != nil {
		zipWriter.Close()
		tmpZip.Close()
		http.Error(w, "Failed to create file in zip: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = io.Copy(csvFileInZip, tmpCSV)
	if err != nil {
		zipWriter.Close()
		tmpZip.Close()
		http.Error(w, "Failed to copy to zip: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = zipWriter.Close()
	if err != nil {
		tmpZip.Close()
		http.Error(w, "Failed to close zip writer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tmpZip.Seek(0, 0)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=data.zip")
	_, err = io.Copy(w, tmpZip)
	tmpZip.Close()

	if err != nil {
		log.Printf("Error writing response: %v", err)
		return
	}
}

func main() {
	if err := initDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()


	r := mux.NewRouter()
	r.HandleFunc("/api/v0/prices", handlePostPrices).Methods("POST")
	r.HandleFunc("/api/v0/prices", handleGetPrices).Methods("GET")

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
