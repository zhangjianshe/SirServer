package sfile

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type Box struct {
	minx float64
	miny float64
	maxx float64
	maxy float64
}

func NewBox() Box {
	return Box{math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64}
}
func (b *Box) extend(b2 Box) {
	b.minx = min(b.minx, b2.minx)
	b.miny = min(b.miny, b2.miny)
	b.maxx = max(b.maxx, b2.maxx)
	b.maxy = max(b.maxy, b2.maxy)
}
func (b *Box) Set(minx float64, miny float64, maxx float64, maxy float64) {
	b.minx = minx
	b.miny = miny
	b.maxx = maxx
	b.maxy = maxy
}
func (b *Box) Empty() {
	b.minx = math.MaxFloat64
	b.miny = math.MaxFloat64
	b.maxx = -math.MaxFloat64
	b.maxy = -math.MaxFloat64
}
func (b *Box) IsEmpty() bool {
	return b.minx == math.MaxFloat64 && b.miny == math.MaxFloat64 && b.maxx == -math.MaxFloat64 && b.maxy == -math.MaxFloat64
}

type Repository struct {
	Name  string  `json:"name"`
	Lng   float64 `json:"lng"`
	Lat   float64 `json:"lat"`
	Zoom  int     `json:"zoom"`
	Size  float64 `json:"size"`
	Url   string  `json:"url"`
	Pared bool    `json:"pared"`
}

// ListRepositories returns a list of available repositories
func ListRepositories(baseDir string) ([]Repository, error) {
	repositories := make([]Repository, 0)
	dirs, error := os.ReadDir(baseDir)
	if error != nil {
		return make([]Repository, 0), error
	}

	for _, dir := range dirs {
		if dir.IsDir() {
			repo, err := readRepositoryInfo(baseDir, dir)
			if err != nil {
				repo, err = analysisRepository(baseDir, dir)
				if err != nil {
					repositories = append(repositories, Repository{
						Name:  dir.Name(),
						Lng:   113.,
						Lat:   40.,
						Size:  0,
						Url:   dir.Name(),
						Pared: false,
						Zoom:  10,
					})
				} else {
					repositories = append(repositories, repo)
				}
			} else {
				repositories = append(repositories, repo)
			}
		}
	}
	return repositories, nil
}

func readRepositoryInfo(baseDir string, entry os.DirEntry) (Repository, error) {
	var repo Repository

	// Construct full path correctly
	fullPath := filepath.Join(baseDir, entry.Name(), "repository.json")

	// Open the file
	file, err := os.Open(fullPath)
	if err != nil {
		return repo, fmt.Errorf("failed to open repository.json: %w", err)
	}
	defer file.Close()

	// Decode JSON
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&repo); err != nil {
		return repo, fmt.Errorf("failed to parse repository.json: %w", err)
	}

	return repo, nil
}
func analysisRepository(baseDir string, entry os.DirEntry) (Repository, error) {
	// Create a default repository with the directory name
	repo := Repository{
		Name:  entry.Name(),
		Lng:   113.0,
		Lat:   40.0,
		Size:  0,
		Url:   entry.Name(),
		Pared: false,
		Zoom:  10,
	}

	box := NewBox()
	subdirs, err := listSubDir(filepath.Join(baseDir, entry.Name()))
	if err != nil {
		return Repository{}, err
	}
	var fileSize float64 = 0
	for _, sub := range subdirs {
		files, err := listAllFile(sub)
		if err != nil {
			return Repository{}, err
		}
		for _, file := range files {
			box1, err := calExtend(file)
			if err != nil {
				continue
			}
			box.extend(box1)
			info, error := os.Stat(file)
			if error != nil {
				continue
			}
			fileSize += float64(info.Size())
		}
	}
	// Construct full path correctly
	fullPath := filepath.Join(baseDir, entry.Name(), "repository.json")

	repo.Pared = true
	repo.Zoom = 14
	repo.Lat = 0.5 * (box.miny + box.maxy)
	repo.Lng = 0.5 * (box.minx + box.maxx)
	repo.Size = fileSize
	// Marshal the repository to JSON
	jsonData, err := json.MarshalIndent(repo, "", "  ")
	if err != nil {
		return Repository{}, fmt.Errorf("failed to marshal repository: %w", err)
	}

	// Create the file
	file, err := os.Create(fullPath)
	if err != nil {
		return Repository{}, fmt.Errorf("failed to create repository.json: %w", err)
	}
	defer file.Close()

	// Write JSON data to file
	_, err = file.Write(jsonData)
	if err != nil {
		return Repository{}, fmt.Errorf("failed to write repository.json: %w", err)
	}

	return repo, nil
}

func listAllFile(dir string) ([]string, error) {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, d := range dirs {
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".s") {
			files = append(files, path.Join(dir, d.Name()))
		}
	}
	return files, nil
}

func listSubDir(dir string) ([]string, error) {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	subDirs := make([]string, 0)
	for _, d := range dirs {
		if d.IsDir() {
			match, err := regexp.MatchString("^[A-Z]$", d.Name())
			if err != nil {
				continue
			}
			if !match {
				continue
			}
			subDirs = append(subDirs, path.Join(dir, d.Name()))
		}
	}
	return subDirs, nil
}

func calExtend(sFilePath string) (Box, error) {
	db, err := sql.Open("sqlite3", sFilePath)
	if err != nil {
		return Box{}, err
	}
	tableNames, err := listTables(db)
	if err != nil {
		return Box{}, err
	}
	box := NewBox()
	for _, tableName := range tableNames {
		var tileXMin int64
		var tileXMax int64
		var tileYMin int64
		var tileYMax int64
		err = db.QueryRow("select min(X), max(X), min(Y), max(Y) from "+tableName).Scan(&tileXMin, &tileXMax, &tileYMin, &tileYMax)
		if err != nil {
			log.Println(err)
			return Box{}, err
		}

		//extend是tile编号的范围，我们需要将其转化为经纬度
		// 编号坐标原点为 左上角 向下 向右生长
		// GlobalMercator 计算方式是 右下角为坐标原点 所以 做个转换
		zoom := int32(tableName[0]) - int32("A"[0])
		minTile := tileBound(tileXMin, tileYMin, zoom)
		maxTile := tileBound(tileXMax, tileYMax, zoom)
		box.extend(minTile)
		box.extend(maxTile)
	}
	return box, nil
}

// 计算tile编号的范围
// xMin: tileX的最小值
// yMin: tileY的最小值
// zoom: 缩放级别
// this method calculate the tile bound based on web mercator tile system
// return tile's bound with wgs84 coordinate
func tileBound(tx int64, ty int64, zoom int32) Box {
	tileSize := 256.
	x, y := pixelsToMeters(float64(tx)*tileSize, float64(ty)*tileSize, zoom)
	x0, y0 := meterToLngLat(x, y)

	x_, y_ := pixelsToMeters(float64(tx+1)*tileSize, float64(ty+1)*tileSize, zoom)
	x1, y1 := meterToLngLat(x_, y_)

	return Box{
		minx: x0,
		miny: y1,
		maxx: x1,
		maxy: y0,
	}
}

var INITIALIZE_RESOLUTION = 2. * math.Pi * 6378137 / 256.

var ORIGIN_SHIFT = 2 * math.Pi * 6378137 / 2.0

func pixelsToMeters(px float64, py float64, zoom int32) (float64, float64) {
	//zoom级别 每个像素对应的地面米数 该值只在赤道上是准确的 其他地方都有偏差，但是这个偏差只是为了对位置的一个描述
	res := INITIALIZE_RESOLUTION / math.Pow(2, float64(zoom))
	//    墨卡托 米     离左上角的米数       墨卡托的坐标原点X
	mx := px*res - ORIGIN_SHIFT
	//     墨卡托 米       离左上角的米数 Y方向      墨卡托的坐标原点Y
	my := -(py*res - ORIGIN_SHIFT)
	return mx, my
}

/**
 * "Maximal scaledown zoom of the pyramid closest to the pixelSize."
 * 墨卡托 以米为单位的坐标 转化为WGS84经纬度坐标
 *
 * @param mx
 * @param my
 * @return
 */
func meterToLngLat(mx float64, my float64) (float64, float64) {
	lon := (mx / ORIGIN_SHIFT) * 180.0
	lat := (my / ORIGIN_SHIFT) * 180.0

	lat = 180 / math.Pi * (2*math.Atan(math.Exp(lat*math.Pi/180.0)) - math.Pi/2.0)
	return lon, lat
}

func listTables(sqlDb *sql.DB) ([]string, error) {
	tableNames := make([]string, 0)
	fetchTablesSql := "select name from sqlite_master where type='table'  order by name"
	query, err := sqlDb.Query(fetchTablesSql)
	if err != nil {
		return tableNames, err
	}
	defer query.Close()
	for query.Next() {
		var tableName string
		err = query.Scan(&tableName)
		if err != nil {
			continue
		}
		if len(strings.Split(tableName, "_")) == 3 {
			tableNames = append(tableNames, tableName)
		}
	}
	return tableNames, nil
}
