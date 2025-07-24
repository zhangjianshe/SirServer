package sfile

import (
	"bytes"
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"os"
	"path/filepath"
)

type SRepository struct {
	dir string
}

// GetXYZ returns the content of the XYZ file
func (f SRepository) GetXYZ(x int64, y int64, z int8) (*bytes.Buffer, error) {
	vz := max(z, 9)
	subDir := fmt.Sprintf("%c", 'A'+vz)
	dbFile := fmt.Sprintf("%c_%d_%d.s", 'A'+vz, x/256, y/256)
	filePath := filepath.Join(f.dir, subDir, dbFile)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Print(err)
		return nil, fmt.Errorf("%s not exist", filePath)
	}
	db, err := sql.Open("sqlite3", filePath)
	if err != nil {
		log.Print(err)
		return nil, err
	}
	defer db.Close()
	tableName := fmt.Sprintf("%c_%d_%d", 'A'+vz, x/64, y/64)
	index := x%64 + 64*(y%64)
	selectSql := fmt.Sprintf("select Data from %s where ID=%d", tableName, index)
	rows, err := db.Query(selectSql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if rows.Next() {
		var data []byte
		err = rows.Scan(&data)
		if err != nil {
			return nil, err
		}
		return bytes.NewBuffer(data), nil
	}
	return nil, fmt.Errorf("%s not exist", filePath)
}

// NewRepository creates a new SRepository
func NewRepository(dir string, created bool) (*SRepository, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		if created {
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				return nil, err
			}
		}
		return &SRepository{dir: dir}, fmt.Errorf("%s not exist", dir)
	}
	if info.IsDir() {
		return &SRepository{dir: dir}, nil
	}
	return nil, fmt.Errorf("%s is not a directory", dir)
}
