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
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	createTableQuery := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			category VARCHAR(255) NOT NULL,
			price DECIMAL(10, 2) NOT NULL,
			create_date TIMESTAMP NOT NULL
		)`, tableName)

	_, err = db.Exec(createTableQuery)
	if err != nil {
		db.Close()
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
		if strings.HasSuffix(f.Name, ".csv") && !strings.Contains(f.Name, "/") {
			rc, err := f.Open()
			if err != nil {
				http.Error(w, "Failed to open CSV file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			csvData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				http.Error(w, "Failed to read CSV file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "CSV file not found in zip archive", http.StatusBadRequest)
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

	type ValidRecord struct {
		Name       string
		Category   string
		Price      float64
		CreateDate string
	}

	var validRecords []ValidRecord

	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 5 {
			continue
		}

		name := record[1]
		category := record[2]
		price, err := strconv.ParseFloat(record[3], 64)
		if err != nil {
			continue
		}

		createDate := record[4]

		validRecords = append(validRecords, ValidRecord{
			Name:       name,
			Category:   category,
			Price:      price,
			CreateDate: createDate,
		})
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Failed to begin transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	insertQuery := fmt.Sprintf("INSERT INTO %s (name, category, price, create_date) VALUES ($1, $2, $3, $4)", tableName)
	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		http.Error(w, "Failed to prepare statement: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, rec := range validRecords {
		_, err = stmt.Exec(rec.Name, rec.Category, rec.Price, rec.CreateDate)
		if err != nil {
			log.Printf("Failed to insert record: %v", err)
			http.Error(w, "Failed to insert record: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var totalItems int
	err = tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&totalItems)
	if err != nil {
		http.Error(w, "Failed to count items: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var totalCategories int
	err = tx.QueryRow(fmt.Sprintf("SELECT COUNT(DISTINCT category) FROM %s", tableName)).Scan(&totalCategories)
	if err != nil {
		http.Error(w, "Failed to count categories: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var totalPrice float64
	err = tx.QueryRow(fmt.Sprintf("SELECT COALESCE(SUM(price), 0) FROM %s", tableName)).Scan(&totalPrice)
	if err != nil {
		http.Error(w, "Failed to sum prices: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Failed to commit transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := UploadResponse{
		TotalItems:      totalItems,
		TotalCategories: totalCategories,
		TotalPrice:      totalPrice,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func fetchAllPrices() ([]PriceRecord, error) {
	query := fmt.Sprintf("SELECT id, name, category, price, create_date FROM %s ORDER BY id", tableName)
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query database: %w", err)
	}
	defer rows.Close()

	var records []PriceRecord
	for rows.Next() {
		var rec PriceRecord
		err := rows.Scan(&rec.ID, &rec.Name, &rec.Category, &rec.Price, &rec.CreateDate)
		if err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}
		records = append(records, rec)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return records, nil
}

func createCSVFile(records []PriceRecord) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)
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

	if err := csvWriter.Error(); err != nil {
		return nil, fmt.Errorf("failed to write CSV: %w", err)
	}

	return &buf, nil
}

func createZipArchive(csvBuffer *bytes.Buffer) (*bytes.Buffer, error) {
	var zipBuffer bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuffer)

	csvFileInZip, err := zipWriter.Create("data.csv")
	if err != nil {
		zipWriter.Close()
		return nil, fmt.Errorf("failed to create file in zip: %w", err)
	}

	_, err = io.Copy(csvFileInZip, csvBuffer)
	if err != nil {
		zipWriter.Close()
		return nil, fmt.Errorf("failed to copy to zip: %w", err)
	}

	err = zipWriter.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	return &zipBuffer, nil
}

func sendZipResponse(w http.ResponseWriter, zipBuffer *bytes.Buffer) error {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=data.zip")
	_, err := io.Copy(w, zipBuffer)
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	return nil
}

func handleGetPrices(w http.ResponseWriter, r *http.Request) {
	records, err := fetchAllPrices()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	csvBuffer, err := createCSVFile(records)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	zipBuffer, err := createZipArchive(csvBuffer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err = sendZipResponse(w, zipBuffer); err != nil {
		log.Printf("Error sending zip response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
