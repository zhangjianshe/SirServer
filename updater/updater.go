package updater

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

// Updater struct holds dependencies and constants for the update process
type Updater struct {
	CurrentVersion  string
	VersionInfoURL  string
	DownloadBaseURL string
}

// NewUpdater creates and returns a new Updater instance
func NewUpdater(currentVersion, versionInfoURL, downloadBaseURL string) *Updater {
	return &Updater{
		CurrentVersion:  currentVersion,
		VersionInfoURL:  versionInfoURL,
		DownloadBaseURL: downloadBaseURL,
	}
}

// PerformUpdate handles the entire update process
func (u *Updater) PerformUpdate() {
	color.Blue("Checking for updates...")

	latestVersion, err := u.getLatestVersionInfo()
	if err != nil {
		color.Red("Failed to get latest version info: %v", err)
		return
	}

	color.Green("Current version: %s", u.CurrentVersion)
	color.Green("Latest version available: %s", latestVersion)

	if u.CurrentVersion == latestVersion {
		color.Yellow("You are already running the latest version.")
		return
	}

	color.Cyan("A new version (%s) is available. Do you want to update? (y/N): ", latestVersion)
	var confirmation string
	_, _ = fmt.Scanln(&confirmation)

	if strings.ToLower(confirmation) != "y" {
		color.Red("Update cancelled by user.")
		return
	}

	serverFileName := "SirServer_Setup.exe"
	// Construct the full download URL
	downloadURL := u.DownloadBaseURL + "/v" + latestVersion + "/" + serverFileName
	color.Blue("Downloading update from: %s", downloadURL)

	zipFilePath, err := u.downloadFile(downloadURL, "")
	if err != nil {
		color.Red("Failed to download update: %v", err)
		return
	}
	defer os.Remove(zipFilePath) // Clean up the downloaded zip file

	color.Green("Update downloaded to: %s", zipFilePath)

	// Get the directory where the current executable is located
	execDir, err := os.Executable()
	if err != nil {
		color.Red("Failed to get executable path: %v", err)
		return
	}
	currentDir := filepath.Dir(execDir)

	color.Blue("Extracting update to: %s", currentDir)
	if err := u.extractAndReplace(zipFilePath, currentDir); err != nil {
		color.Red("Failed to extract and replace files: %v", err)
		return
	}

	color.Green("Update successful! Please restart SirServer to apply the changes.")
}

// getLatestVersionInfo fetches the latest version string and download filename from the given URL.
func (u *Updater) getLatestVersionInfo() (string, error) {
	resp, err := http.Get(u.VersionInfoURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch version info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch version info: HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read version info response: %w", err)
	}

	// Assuming the file content is "v0.0.1"
	return strings.Trim(string(body), "v\r\n"), nil
}

// downloadFile downloads a file from a URL to a temporary location.
func (u *Updater) downloadFile(url string, filename string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download file: HTTP status %d", resp.StatusCode)
	}

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", filename+"-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tmpFile.Close() // Ensure it's closed

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		os.Remove(tmpFile.Name()) // Clean up on error
		return "", fmt.Errorf("failed to write downloaded file: %w", err)
	}

	return tmpFile.Name(), nil
}

// extractAndReplace extracts the zip file and replaces the current application files.
func (u *Updater) extractAndReplace(zipFilePath, destDir string) error {
	r, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)

		// Check for ZipSlip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("zipslip vulnerability detected: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			err = os.MkdirAll(fpath, os.ModePerm)
			if err != nil {
				return fmt.Errorf("failed to create directory %s: %w", fpath, err)
			}
			continue
		}

		// Create the file
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", fpath, err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to copy data for file %s: %w", f.Name, err)
		}

		// Handle executable permissions for the main binary
		// We use os.Args[0] to get the running executable's name.
		// Note: On Windows, the executable might be locked while running,
		// making direct replacement difficult. For robust updates, consider
		// a separate bootstrapping process or techniques like renaming the
		// old binary and renaming the new one into place.
		if f.Name == filepath.Base(os.Args[0]) {
			if err := os.Chmod(fpath, 0755); err != nil { // Make it executable
				color.Yellow("Warning: Could not set executable permission for %s: %v", fpath, err)
			}
		}
	}
	return nil
}
