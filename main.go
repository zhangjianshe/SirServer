package main

import (
	"SirServer/api" // Import the api package
	"SirServer/canvas"
	"SirServer/updater" // Import the updater package
	"embed"
	"fmt"
	"log"      // For logging errors
	"net/http" // Standard HTTP package
	"os"       // For exiting
	"path/filepath"
	"runtime"
	"strings" // For string manipulation

	"github.com/fatih/color" // For colored console output
	"github.com/gorilla/mux" // Web router
	"github.com/spf13/cobra" // Cobra for CLI
)

//go:embed static/*
var staticFiles embed.FS

var APP_VERSION = "0.0.49" // Current application version

var DefaultRepositoryRoot string

// sirServer global variable initialized with server metadata
var sirServer = api.SirServer{ // Use api.SirServer from the api package
	Name:    "SirServer",
	Version: APP_VERSION,
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
		fmt.Printf("%s version %s\n", sirServer.Name, APP_VERSION)
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
	err := http.ListenAndServe(listenAddr, r)
	if err != nil {
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
	cyan := color.New(color.FgCyan)
	whiteBold := color.New(color.FgWhite, color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	// Define a fixed width for the banner for simplicity and consistent layout
	const bannerWidth = 70

	// Top border
	cyan.Println("╔" + strings.Repeat("═", bannerWidth-2) + "╗")

	// Title line (centered based on plain string length)
	titleContent := fmt.Sprintf("%s %s", sirServer.Name, sirServer.Version)
	titlePaddingLeft := (bannerWidth - 2 - len(titleContent)) / 2
	titlePaddingRight := bannerWidth - 2 - len(titleContent) - titlePaddingLeft
	fmt.Printf("║%s%s%s║\n",
		strings.Repeat(" ", titlePaddingLeft),
		whiteBold.Sprint(titleContent),
		strings.Repeat(" ", titlePaddingRight))

	// Separator
	cyan.Println("╠" + strings.Repeat("═", bannerWidth-2) + "╣")

	// Empty line for spacing
	fmt.Printf("║%s║\n", strings.Repeat(" ", bannerWidth-2))

	// Author line (left-aligned with consistent indentation)
	// We want 4 spaces after the '║' border.
	authorContent := fmt.Sprintf("Author: %s", sirServer.Author)
	authorRightPadding := bannerWidth - 2 - 4 - len(authorContent) // total width - borders - left_indent - content_length
	fmt.Printf("║    %s%s║\n", green.Sprint(authorContent), strings.Repeat(" ", authorRightPadding))

	// Email line (left-aligned with consistent indentation)
	emailContent := fmt.Sprintf("Email: %s", sirServer.Email)
	emailRightPadding := bannerWidth - 2 - 4 - len(emailContent) // total width - borders - left_indent - content_length
	fmt.Printf("║    %s%s║\n", yellow.Sprint(emailContent), strings.Repeat(" ", emailRightPadding))

	// Empty line for spacing
	fmt.Printf("║%s║\n", strings.Repeat(" ", bannerWidth-2))

	// Bottom border
	cyan.Println("╚" + strings.Repeat("═", bannerWidth-2) + "╝")
	fmt.Println() // Add a newline for better spacing
}
