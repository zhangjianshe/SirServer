package api

import (
	"SirServer/canvas" // Assuming canvas is a sibling package
	"SirServer/sfile"  // Assuming sfile is a sibling package
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"image"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
)

// SirServer struct defines the server's metadata (moved here from main.go)
type SirServer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Author  string `json:"author"`
	Email   string `json:"email"`
}

// ApiResult struct defines a standard API response format (moved here from main.go)
type ApiResult struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// Ok creates a successful API result (moved here from main.go)
func Ok(data interface{}) ApiResult {
	return ApiResult{
		Code:    0,
		Message: "success",
		Data:    data,
	}
}

// Error creates an error API result (moved here from main.go)
func Error(code int, message string) ApiResult {
	return ApiResult{
		Code:    code,
		Message: message,
		Data:    nil,
	}
}

// WriteOk marshals data into a successful JSON response and writes it to the http.ResponseWriter (moved here)
func WriteOk(writer http.ResponseWriter, data interface{}) {
	writer.Header().Set("Content-Type", "application/json")
	result, err := json.Marshal(Ok(data))
	if err != nil {
		log.Printf("Error marshalling success response: %v", err)
		errorJson, _ := json.Marshal(Error(http.StatusInternalServerError, "Internal server error during response marshalling"))
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write(errorJson)
		return
	}
	_, _ = writer.Write(result)
}

// WriteError marshals an error into a JSON response and writes it to the http.ResponseWriter (moved here)
func WriteError(writer http.ResponseWriter, code int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(code)
	result, err := json.Marshal(Error(code, message))
	if err != nil {
		log.Printf("Error marshalling error response: %v", err)
		fallbackErrorJson, _ := json.Marshal(Error(http.StatusInternalServerError, "Internal server error"))
		_, _ = writer.Write(fallbackErrorJson)
		return
	}
	_, _ = writer.Write(result)
}

// WriteImage writes an image buffer as a PNG response (moved here)
func WriteImage(writer http.ResponseWriter, buffer bytes.Buffer) {
	writer.Header().Set("Content-Type", "image/png")
	writer.WriteHeader(http.StatusOK)
	writer.Header().Set("Content-Length", strconv.Itoa(len(buffer.Bytes())))
	_, _ = writer.Write(buffer.Bytes())
}

// WriteHtml writes HTML content (moved here)
func WriteHtml(writer http.ResponseWriter, content []byte) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	writer.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = writer.Write(content)
}

// ApiContext holds dependencies for API handlers
type ApiContext struct {
	RepositoryRoot string
	SirServerInfo  SirServer
	CanvasContext  *canvas.CanvasContext // Note: canvas.CanvasContext is not an interface, so we pass the concrete type
	StaticFiles    embed.FS
}

// NewApiContext creates and returns a new ApiContext
func NewApiContext(repoRoot string, serverInfo SirServer, canvasCtx *canvas.CanvasContext, staticFs embed.FS) *ApiContext {
	return &ApiContext{
		RepositoryRoot: repoRoot,
		SirServerInfo:  serverInfo,
		CanvasContext:  canvasCtx,
		StaticFiles:    staticFs,
	}
}

// RegisterRoutes registers all API routes to the given mux router
func (ac *ApiContext) RegisterRoutes(r *mux.Router) {
	// API Routes
	r.HandleFunc("/api/v1/repositories", ac.listRepositoriesHandler).Methods("GET")
	r.HandleFunc("/api/v1/xyz/{dir}/{z:[0-9]+}/{x:[0-9]+}/{y:[0-9]+}.png", ac.xyzFileHandler).Methods("GET")
	r.HandleFunc("/api/v1/server", ac.serverInfoHandler).Methods("GET")

	// Root path (index.html) - depends on static files, so keep it in this context if it references embedded static files
	// If index.html is truly static and doesn't depend on server-side logic related to repo or canvas,
	// it could be served directly by the main application's static file handler.
	// For now, keeping it here as it was logically grouped with other content serving.
	staticFileDirectory := "static" // Path inside the embedded FS
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexFile, _ := url.JoinPath(staticFileDirectory, "index.html")
		content, err := ac.StaticFiles.ReadFile(indexFile)
		if err != nil {
			WriteHtml(w, []byte("404 Not Found"))
			return
		}
		WriteHtml(w, content)
	})
}

// listRepositoriesHandler provides a list of available repositories
func (ac *ApiContext) listRepositoriesHandler(writer http.ResponseWriter, request *http.Request) {
	repositories, err := sfile.ListRepositories(ac.RepositoryRoot)
	if err != nil {
		WriteError(writer, http.StatusInternalServerError, "Failed to list repositories")
		return
	}
	WriteOk(writer, repositories)
}

// xyzFileHandler processes requests for XYZ files
func (ac *ApiContext) xyzFileHandler(writer http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	fmt.Printf("Received request for XYZ: %v\n", vars)
	dirName := vars["dir"]
	x := vars["x"]
	y := vars["y"]
	z := vars["z"]
	dir := filepath.Join(ac.RepositoryRoot, dirName)
	NewSFile, err := sfile.NewRepository(dir, false)
	if err != nil {
		// Use ac.CanvasContext
		buffer, _ := ac.CanvasContext.CreateImage(256, 256, image.Transparent, image.Black, err.Error())
		WriteImage(writer, buffer)
		return
	}
	intx, _ := strconv.ParseInt(x, 10, 64)
	inty, _ := strconv.ParseInt(y, 10, 64)
	intz, _ := strconv.ParseInt(z, 10, 8)

	xyz, err := NewSFile.GetXYZ(intx, inty, int8(intz))
	if err != nil {
		// Use ac.CanvasContext
		buffer, _ := ac.CanvasContext.CreateImage(256, 256, image.Transparent, image.Black, err.Error())
		WriteImage(writer, buffer)
		return
	}
	WriteImage(writer, *xyz)
}

// serverInfoHandler provides information about the server
func (ac *ApiContext) serverInfoHandler(writer http.ResponseWriter, request *http.Request) {
	WriteOk(writer, ac.SirServerInfo)
}
