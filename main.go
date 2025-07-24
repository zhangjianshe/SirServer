package main

import (
	"SirServer/api" // Import the api package
	"SirServer/canvas"
	"SirServer/updater" // Import the updater package
	"embed"
	"fmt"
	"github.com/fatih/color" // For colored console output
	"github.com/gorilla/mux" // Web router
	"github.com/spf13/cobra" // Cobra for CLI
	"log"                    // For logging errors
	"net/http"               // Standard HTTP package
	"os"                     // For exiting
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

var AppVersion = "0.0.56" // Current application version

var DefaultRepositoryRoot string

// sirServer global variable initialized with server metadata
var sirServer = api.SirServer{ // Use api.SirServer from the api package
	Name:    "SirServer",
	Version: AppVersion,
	Author:  "Zhang JianShe",
	Email:   "zhangjianshe@gmail.com",
}

// canvasContext and REPOSITORY_ROOT are still in main as they are core to this main application's setup
var canvasContext = canvas.NewCanvasContext(staticFiles)

// Global variables for flags (will be populated by Cobra)
var (
	repositoryRoot string
	port           int
)

// Update URLs (passed to updater package)
const (
	versionInfoURL  = "https://lc.cangling.cn:22002/api/v1/file/read/1b7190d20796b87e6e528748e0d8b45a4d8b5899fdd3c46301c3973713da90cd/version.json"
	downloadBaseURL = "https://lc.cangling.cn:22002/api/v1/file/read/1b7190d20796b87e6e528748e0d8b45a4d8b5899fdd3c46301c3973713da90cd"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "SirServer",
	Short: "A server for image repositories and tile data",
	Long: `SirServer is a powerful and efficient server designed to host
image repositories and serve XYZ tile data for mapping applications.
It also provides an integrated update mechanism.`,
	Run: func(cmd *cobra.Command, args []string) {
		// If no subcommand is given, default to running the 'serve' command
		// This makes 'SirServer' equivalent to 'SirServer serve'
		serveCmd.Run(cmd, args)
	},
}

// serveCmd represents the 'serve' subcommand
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the SirServer HTTP server",
	Long:  `Starts the SirServer HTTP server to serve image repositories and API endpoints.`,
	Run:   runServer, // The function that actually starts the server
}

// updateCmd represents the 'update' subcommand
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and perform application update",
	Long:  `Downloads the latest version of SirServer and replaces the current executable.`,
	Run:   runUpdate, // The function that handles the update process
}

// versionCmd represents the 'version' subcommand
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of SirServer",
	Long:  `All software has versions. This is SirServer's.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s version %s\n", sirServer.Name, AppVersion)
	},
}

func init() {
	// Local flags for the 'serve' command
	serveCmd.Flags().StringVarP(&repositoryRoot, "repo-root", "r", DefaultRepositoryRoot, "Root directory for image repositories")
	serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to listen on")

	// Add subcommands to the root command
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd) // Add the new version command
}

func getCurrentDirectory() (string, error) {
	// Method 1: Current Working Directory
	cwdDir, cwdErr := os.Getwd()

	// Method 2: Executable Directory
	exePath, exeErr := os.Executable()
	exeDir := filepath.Dir(exePath)

	// Method 3: Caller's File Directory
	_, filename, _, callerOk := runtime.Caller(0)
	callerDir := filepath.Dir(filename)

	// Choose the most appropriate method
	if cwdErr == nil && cwdDir != "" {
		return cwdDir, nil
	}

	if exeErr == nil && exeDir != "" {
		return exeDir, nil
	}

	if callerOk {
		return callerDir, nil
	}

	return "", fmt.Errorf("could not determine current directory")
}
func main() {
	printBanner() // Print the server banner
	DefaultRepositoryRoot, _ = getCurrentDirectory()
	// Execute the root command. Cobra will handle parsing args and calling the right command.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runServer contains the logic to start the HTTP server
func runServer(cmd *cobra.Command, args []string) {
	listenAddr := fmt.Sprintf("0.0.0.0:%d", port)

	if repositoryRoot == DefaultRepositoryRoot {
		color.Red("Warning: Using default repository root: %s", DefaultRepositoryRoot)
		color.Red("You can change this by using the --repo-root or -r flag: ")
		color.Green("./SirServer serve --repo-root /path/to/repositories -p 8080")
		color.Cyan("\r\n")
	}

	r := mux.NewRouter()

	// --- Static File Serving for /static/ prefix ---
	fs := http.FileServer(http.FS(staticFiles))
	r.PathPrefix("/static/").Handler(http.StripPrefix("", fs))

	// Debugging route
	r.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		entries, _ := staticFiles.ReadDir("static")
		fmt.Fprintln(w, "Embedded Files:")
		for _, entry := range entries {
			fmt.Fprintf(w, "- %s\n", entry.Name())
		}
	})

	// Initialize the API context with necessary dependencies
	// Note: We use the global 'repositoryRoot' variable populated by Cobra
	apiCtx := api.NewApiContext(repositoryRoot, sirServer, canvasContext, staticFiles)

	// Register all API routes using the apiCtx
	apiCtx.RegisterRoutes(r)

	// Start the HTTP server
	color.Blue("\nClick link to open browser: http://localhost:%d\n", port)
	color.Cyan("\n")
	log.Printf("SirServer listening on %s", listenAddr)

	// Start the HTTP server in a goroutine so it doesn't block
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("SirServer listening on %s", listenAddr)
		serverErrors <- http.ListenAndServe(listenAddr, r)
	}()

	// Give the server a moment to start up before trying to open the browser
	time.Sleep(500 * time.Millisecond) // Added time.Sleep for server startup

	// Attempt to open the browser
	browserURL := fmt.Sprintf("http://localhost:%d", port)
	color.Blue("\nClick link to open browser: %s\n", browserURL) // Keep this line for manual fallback
	color.Cyan("\n")                                             // Added new line for spacing

	if err := OpenBrowser(browserURL); err != nil {
		log.Printf("Warning: Could not automatically open browser: %v", err)
		fmt.Println("Please open your web browser manually and navigate to:", browserURL)
	} else {
		log.Println("Browser launched successfully (or command sent).")
	}

	// Wait for the server to exit (e.g., due to an error or signal)
	if err := <-serverErrors; err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// runUpdate contains the logic to perform the application update
func runUpdate(cmd *cobra.Command, args []string) {
	appUpdater := updater.NewUpdater(sirServer.Version, versionInfoURL, downloadBaseURL)
	appUpdater.PerformUpdate()
}

// printBanner prints a simple and robust banner to the console
func printBanner() {
	fmt.Println("╔════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                                SirServer                           ║")
	fmt.Println("╠════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║                                                                    ║")
	fmt.Println("║ Author :  Zhang JianShe                                            ║")
	fmt.Println("║ Email  :  zhangjianshe@gmail.com                                   ║")
	fmt.Println("║                                                                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════════╝")
	fmt.Printf("SirServer Version: %s \n", AppVersion) // Display it here too

}

// OpenBrowser attempts to open the given URL in the default web browser
// based on the operating system.
func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		// On Windows, use "start" command to open the URL in the default browser.
		// "/c" is used with cmd.exe to execute the command and then terminate.
		// "start" is a built-in command, so we need to run it via cmd.exe.
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin": // macOS
		// On macOS, use "open" command.
		cmd = "open"
		args = []string{url}
	case "linux":
		// On Linux, try "xdg-open" first, then "sensible-browser", then "gnome-open", etc.
		// xdg-open is the recommended way to open files/URLs in the user's preferred application.
		cmd = "xdg-open"
		args = []string{url}
		// Fallback options if xdg-open is not available (less common now, but good for robustness)
		// Consider adding more robust fallback logic if xdg-open is frequently missing in your target environments.
		// For simplicity, we'll stick to xdg-open as the primary.
		// If xdg-open fails, you might try:
		// cmd = "sensible-browser"
		// cmd = "gnome-open"
		// cmd = "kde-open"
		// cmd = "x-www-browser"
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	// Create a new command.
	// We don't need to capture stdout/stderr unless we want to debug specific browser errors.
	// For simply launching, this is sufficient.
	c := exec.Command(cmd, args...)

	// Run the command.
	// Use c.Start() if you want to launch the browser asynchronously and not wait for it to close.
	// Use c.Run() if you want to wait for the command to complete (e.g., browser process to exit),
	// but for a browser, Start() is usually more appropriate.
	err := c.Start()
	if err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}

	log.Printf("Attempted to open URL: %s using command: %s %v", url, cmd, args)
	return nil
}
