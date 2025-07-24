package main

import (
	"SirServer/canvas"
	"SirServer/sfile"
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/fatih/color" // For colored console output
	"github.com/gorilla/mux" // Web router
	"image"
	"log"           // For logging errors
	"net/http"      // Standard HTTP package
	"path/filepath" // For joining file paths safely
	"strconv"
	"strings" // For string manipulation
)

//go:embed static/*
var staticFiles embed.FS

var APP_VERSION = "0.0.28"

// SirServer struct defines the server's metadata
type SirServer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Author  string `json:"author"`
	Email   string `json:"email"`
}

// ApiResult struct defines a standard API response format
type ApiResult struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// Ok creates a successful API result
func Ok(data interface{}) ApiResult {
	return ApiResult{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

// Error creates an error API result
func Error(code int, message string) ApiResult {
	return ApiResult{
		Code:    code,
		Message: message,
		Data:    nil,
	}
}

// WriteOk marshals data into a successful JSON response and writes it to the http.ResponseWriter
func WriteOk(writer http.ResponseWriter, data interface{}) {
	// Set Content-Type header to application/json
	writer.Header().Set("Content-Type", "application/json")

	// Marshal the successful API result
	result, err := json.Marshal(Ok(data))
	if err != nil {
		// If marshalling the success response fails, try to marshal a generic error
		log.Printf("Error marshalling success response: %v", err) // Log the internal error
		errorJson, _ := json.Marshal(Error(http.StatusInternalServerError, "Internal server error during response marshalling"))
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write(errorJson)
		return
	}
	// Write the successful JSON response
	_, _ = writer.Write(result)
}

// WriteError marshals an error into a JSON response and writes it to the http.ResponseWriter
func WriteError(writer http.ResponseWriter, code int, message string) {
	// Set Content-Type header to application/json
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(code) // Set the HTTP status code

	// Marshal the error API result
	result, err := json.Marshal(Error(code, message))
	if err != nil {
		// If marshalling the error response fails, log and write a fallback error
		log.Printf("Error marshalling error response: %v", err) // Log the internal error
		fallbackErrorJson, _ := json.Marshal(Error(http.StatusInternalServerError, "Internal server error"))
		_, _ = writer.Write(fallbackErrorJson)
		return
	}
	// Write the error JSON response
	_, _ = writer.Write(result)
}

// sirServer global variable initialized with server metadata
var sirServer = SirServer{
	Name:    "SirServer",
	Version: APP_VERSION,
	Author:  "Zhang JianShe",
	Email:   "zhangjianshe@gmail.com",
}

var canvasContext = canvas.NewCanvasContext(staticFiles)
var REPOSITORY_ROOT = "/mnt/cangling/devdata/share"

func main() {
	printBanner() // Print the server banner

	// process args
	// parse --repository -r
	// parse --port -p
	// parse --help -h

	var (
		repoRoot string
		port     int
	)
	DefaultRepositoryRoot := "/mnt/cangling/devdata/share"
	flag.StringVar(&repoRoot, "r", DefaultRepositoryRoot, "Root directory for image repositories (shorthand)")
	flag.IntVar(&port, "p", 8080, "Port to listen on (shorthand)")
	flag.Parse()

	// Assign to global REPOSITORY_ROOT and use 'port' for ListenAndServe
	REPOSITORY_ROOT = repoRoot
	listenAddr := fmt.Sprintf("0.0.0.0:%d", port)

	if REPOSITORY_ROOT == DefaultRepositoryRoot {
		color.Red("Warning: Using default repository root: %s", DefaultRepositoryRoot)
		color.Red("you can change this by using the -r flag ")
		color.Green("./SirServer -r /path/to/repositories -p 8080")
		color.Cyan("\r\n")
	}

	// --- Static File Serving ---
	// Define the directory where static files are located
	staticFileDirectory := "./static"

	// Create an http.FileServer handler to serve files from the static directory
	fs := http.FileServer(http.FS(staticFiles))

	r := mux.NewRouter()
	// Register the static file handler.
	// PathPrefix("/static/") matches any URL starting with /static/.
	// StripPrefix("/static/", fs) removes the /static/ prefix before passing
	// the path to the FileServer, so it correctly maps to the file system.
	r.PathPrefix("/static/").Handler(http.StripPrefix("", fs))

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		content, err := staticFiles.ReadFile(filepath.Join(staticFileDirectory, "index.html"))
		if err != nil {
			WriteHtml(w, []byte("404 Not Found"))
			return
		}
		WriteHtml(w, content)
	})

	//影像仓库列表
	r.HandleFunc("/api/v1/repositories", listRepositoriesHandler).Methods("GET")

	// --- API Routes ---
	// Route for getting server information
	r.HandleFunc("/api/v1/server", serverInfoHandler).Methods("GET")

	// Route for getting XYZ file information
	// FIX: Corrected the regular expression for x, y, and z variables.
	// It should be `[0-9]+` inside the curly braces for a digit match.
	r.HandleFunc("/api/v1/xyz/{dir}/{z:[0-9]+}/{x:[0-9]+}/{y:[0-9]+}.png", xyzFileHandler).Methods("GET")

	// Start the HTTP server
	color.Blue("\nclick link to open browser: http://localhost:%d\n", port)
	color.Cyan("\n")
	err := http.ListenAndServe(listenAddr, r)
	if err != nil {
		flag.PrintDefaults()
		log.Fatal(err)
	}

}

// listRepositoriesHandler provides a list of available repositories
// @Summary List repositories
// @Description Returns a list of available repositories.
// @Tags files
// @Produce json
func listRepositoriesHandler(writer http.ResponseWriter, request *http.Request) {
	repositories, err := sfile.ListRepositories(REPOSITORY_ROOT)
	if err != nil {
		WriteError(writer, http.StatusInternalServerError, "Failed to list repositories")
		return
	}
	WriteOk(writer, repositories)
}

// xyzFileHandler processes requests for XYZ files
// @Summary Get xyz file
// @Description Extracts directory and XYZ coordinates from the URL and returns them as JSON.
// @Tags files
// @Produce json
// @Param dir path string true "Directory name"
// @Param x path int true "X coordinate"
// @Param y path int true "Y coordinate"
// @Param z path int true "Z coordinate"
// @Success 200 {object} ApiResult{data=map[string]string} "Successfully extracted variables"
// @Failure 500 {object} ApiResult "Internal server error"
// @Router /api/v1/xyz/{dir}/{x}/{y}/{z}.png [get]
func xyzFileHandler(writer http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)                          // Get path variables from the request
	fmt.Printf("Received request for XYZ: %v\n", vars) // Log the extracted variables
	dirName := vars["dir"]
	x := vars["x"]
	y := vars["y"]
	z := vars["z"]
	dir := filepath.Join(REPOSITORY_ROOT, dirName)
	NewSFile, err := sfile.NewRepository(dir, false)
	if err != nil {
		buffer, _ := canvasContext.CreateImage(256, 256, image.Transparent, image.Black, err.Error())
		WriteImage(writer, buffer)
		return
	}
	intx, _ := strconv.ParseInt(x, 10, 64)
	inty, _ := strconv.ParseInt(y, 10, 64)
	intz, _ := strconv.ParseInt(z, 10, 8)

	xyz, err := NewSFile.GetXYZ(intx, inty, int8(intz))
	if err != nil {
		buffer, _ := canvasContext.CreateImage(256, 256, image.Transparent, image.Black, err.Error())
		WriteImage(writer, buffer)
		return
	}
	WriteImage(writer, *xyz)
}

func WriteImage(writer http.ResponseWriter, buffer bytes.Buffer) {
	writer.Header().Set("Content-Type", "image/png")
	writer.WriteHeader(http.StatusOK)
	writer.Header().Set("Content-Length", strconv.Itoa(len(buffer.Bytes())))
	_, _ = writer.Write(buffer.Bytes())
}
func WriteHtml(writer http.ResponseWriter, content []byte) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = writer.Write(content)
}

// serverInfoHandler provides information about the server
// @Summary Get server info
// @Description Returns basic information about the running server.
// @Tags server
// @Produce json
// @Success 200 {object} ApiResult{data=SirServer} "Server information"
// @Router /api/v1/server [get]
func serverInfoHandler(writer http.ResponseWriter, request *http.Request) {
	// The Content-Type is now set by WriteOk, so no need to set it here directly.
	WriteOk(writer, sirServer) // Use the helper to write a successful JSON response
}

// printBanner prints a stylized banner to the console
func printBanner() {
	title := fmt.Sprintf("%s %s", sirServer.Name, sirServer.Version)
	color.Cyan(title)
	color.Cyan(strings.Repeat("=", len(title)))
	color.Cyan("Author: %s", sirServer.Author)
	color.Cyan("Email: %s", sirServer.Email)
	fmt.Println() // Add a newline for better spacing
}
