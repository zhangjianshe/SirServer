package updater

import (
	"archive/tar"   // For Linux .tar.gz
	"archive/zip"   // For Windows .zip
	"compress/gzip" // For Linux .tar.gz
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/blang/semver"              // For semantic version comparison
	"github.com/fatih/color"               // For colored output
	"github.com/inconshreveable/go-update" // The core update library
)

// UpdateInfo reflects the structure of your update_info.json
type UpdateInfo struct {
	LatestVersion string     `json:"latest_version"`
	MinVersion    string     `json:"min_version"`
	Downloads     []Download `json:"downloads"`
}

// Download represents a single downloadable binary entry
type Download struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"` // e.g., "SirServer-linux-amd64.tar.gz" or "SirServer-windows-amd64.zip"
	ID       string `json:"id"`       // The ID your file server uses to retrieve the file
}

// Updater struct holds dependencies and constants for the update process
type Updater struct {
	CurrentVersion  string
	VersionInfoURL  string // URL to your update_info.json
	DownloadBaseURL string // Base URL for file downloads, e.g., "https://lc.cangling.cn:22002/api/v1/file/download/"
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
// This function will attempt to update and then exit the application.
// The caller (e.g., main function) should then restart the application.
func (u *Updater) PerformUpdate() error {
	color.Blue("Checking for updates...")

	// 1. Get latest update information
	updateInfo, err := u.getUpdateInfo()
	if err != nil {
		return fmt.Errorf("failed to get update info: %w", err)
	}

	color.Green("Current version: %s", u.CurrentVersion)
	color.Green("Latest available: %s", updateInfo.LatestVersion)

	currentSemVer, err := semver.ParseTolerant(u.CurrentVersion)
	if err != nil {
		return fmt.Errorf("failed to parse current version '%s': %w", u.CurrentVersion, err)
	}
	latestSemVer, err := semver.ParseTolerant(updateInfo.LatestVersion)
	if err != nil {
		return fmt.Errorf("failed to parse latest version from server '%s': %w", updateInfo.LatestVersion, err)
	}
	minSemVer, err := semver.ParseTolerant(updateInfo.MinVersion)
	if err != nil {
		return fmt.Errorf("failed to parse minimum version from server '%s': %w", updateInfo.MinVersion, err)
	}

	// 2. Check if an update is needed
	if latestSemVer.LTE(currentSemVer) {
		color.Yellow("You are already running the latest version.")
		return nil // No update needed
	}

	// 3. Check minimum version compatibility
	if currentSemVer.LT(minSemVer) {
		color.Red("Your current version (%s) is too old to auto-update to %s (minimum required: %s). Please update manually.",
			u.CurrentVersion, updateInfo.LatestVersion, updateInfo.MinVersion)
		return nil // Not an error, just can't update automatically
	}

	// 4. Confirm with user
	color.Cyan("A new version (%s) is available. Do you want to update? (y/N): ", updateInfo.LatestVersion)
	var confirmation string
	_, _ = fmt.Scanln(&confirmation)

	if strings.ToLower(confirmation) != "y" {
		color.Red("Update cancelled by user.")
		return nil // User cancelled
	}

	// 5. Find the appropriate download
	var targetDownload *Download
	for _, dl := range updateInfo.Downloads {
		if dl.OS == runtime.GOOS && dl.Arch == runtime.GOARCH {
			targetDownload = &dl
			break
		}
	}

	if targetDownload == nil {
		return fmt.Errorf("no update binary found for your system (%s/%s) in the update info", runtime.GOOS, runtime.GOARCH)
	}

	// Construct the full download URL using the ID from the JSON
	downloadURL := filepath.Join(u.DownloadBaseURL, updateInfo.LatestVersion, targetDownload.Filename)
	color.Blue("Downloading update from: %s", downloadURL)

	// 6. Download the archive/binary
	newBinaryReader, err := u.downloadAndPrepareBinary(downloadURL, targetDownload.Filename)
	if err != nil {
		return fmt.Errorf("failed to download and prepare new binary: %w", err)
	}
	defer func() {
		// If newBinaryReader is an io.ReadCloser, ensure it's closed
		if closer, ok := newBinaryReader.(io.ReadCloser); ok {
			closer.Close()
		}
	}()

	// 7. Apply the update using go-update
	color.Blue("Applying update...")
	err = update.Apply(newBinaryReader, update.Options{})
	if err != nil {
		return fmt.Errorf("failed to apply update: %w", err)
	}

	color.Green("Update successful! Please restart SirServer to apply the changes.")
	// It's crucial to exit after a successful update so the OS can load the new binary.
	os.Exit(0)
	return nil // Unreachable
}

// getUpdateInfo fetches and parses the update_info.json
func (u *Updater) getUpdateInfo() (*UpdateInfo, error) {
	resp, err := http.Get(u.VersionInfoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch version info from %s: %w", u.VersionInfoURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch version info, HTTP status: %s", resp.Status)
	}

	var updateInfo UpdateInfo
	if err := json.NewDecoder(resp.Body).Decode(&updateInfo); err != nil {
		return nil, fmt.Errorf("failed to decode update info JSON: %w", err)
	}
	return &updateInfo, nil
}

// downloadAndPrepareBinary downloads the specified file and returns an io.Reader for the extracted executable.
func (u *Updater) downloadAndPrepareBinary(url, filename string) (io.Reader, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close() // Keep this, but we'll read into a temp file first

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file, HTTP status %s", resp.Status)
	}

	// Create a temporary file to store the downloaded content
	// Important: The temp file must remain open until go-update consumes it,
	// or until we extract its content to another temporary file/reader.
	tmpDownloadedFile, err := os.CreateTemp("", "sirserver-update-download-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary download file: %w", err)
	}
	// We do NOT defer os.Remove(tmpDownloadedFile.Name()) here, because we might need it for zip/tar reading.
	// We'll clean it up after we're done with its content.

	_, err = io.Copy(tmpDownloadedFile, resp.Body)
	if err != nil {
		tmpDownloadedFile.Close()           // Close before removing
		os.Remove(tmpDownloadedFile.Name()) // Clean up on error
		return nil, fmt.Errorf("failed to write downloaded content to temporary file: %w", err)
	}
	tmpDownloadedFile.Close() // Close after writing all content

	// Now open the downloaded temporary file for reading and decompression/extraction
	tempFileForReading, err := os.Open(tmpDownloadedFile.Name())
	if err != nil {
		os.Remove(tmpDownloadedFile.Name()) // Clean up
		return nil, fmt.Errorf("failed to open temporary downloaded file for reading: %w", err)
	}
	// Defer its closure
	defer func() {
		tempFileForReading.Close()
		os.Remove(tmpDownloadedFile.Name()) // Clean up the raw downloaded file
	}()

	var newBinaryReader io.Reader
	executableName := filepath.Base(os.Args[0]) // Get the name of the current running executable

	if strings.HasSuffix(filename, ".tar.gz") {
		color.Blue("Decompressing .tar.gz archive...")
		gzr, err := gzip.NewReader(tempFileForReading) // Read from the temp file
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break // End of archive
			}
			if err != nil {
				return nil, fmt.Errorf("failed to read tar header: %w", err)
			}

			// Look for the actual executable inside the tar.gz.
			// Compare base names to handle cases where tar entries might have relative paths.
			if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == executableName {
				// We need to copy this content to another temporary reader that go-update can consume.
				// This is because the tar.Reader will advance, and go-update needs a fresh start.
				tmpExeFile, err := os.CreateTemp("", "sirserver-extracted-*.tmp")
				if err != nil {
					return nil, fmt.Errorf("failed to create temp exe file for tar: %w", err)
				}
				if _, err := io.Copy(tmpExeFile, tr); err != nil {
					tmpExeFile.Close()
					os.Remove(tmpExeFile.Name())
					return nil, fmt.Errorf("failed to copy extracted tar entry to temp file: %w", err)
				}
				tmpExeFile.Seek(0, io.SeekStart) // Rewind for go-update
				newBinaryReader = tmpExeFile
				// We defer removal of tmpExeFile inside the defer of the outer function
				defer func() {
					tmpExeFile.Close()
					os.Remove(tmpExeFile.Name())
				}()
				break
			}
		}
		if newBinaryReader == nil {
			return nil, fmt.Errorf("could not find executable (%s) inside .tar.gz archive", executableName)
		}
	} else if strings.HasSuffix(filename, ".zip") {
		color.Blue("Decompressing .zip archive...")
		zipReader, err := zip.OpenReader(tmpDownloadedFile.Name()) // Open zip from the downloaded temp file
		if err != nil {
			return nil, fmt.Errorf("failed to open zip file: %w", err)
		}
		defer zipReader.Close() // Close the zip reader when done

		var exeFile *zip.File
		for _, f := range zipReader.File {
			if !f.FileInfo().IsDir() && filepath.Base(f.Name) == executableName {
				exeFile = f
				break
			}
		}
		if exeFile == nil {
			return nil, fmt.Errorf("could not find executable (%s) inside .zip archive", executableName)
		}

		rc, err := exeFile.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open executable in zip: %w", err)
		}
		// Similar to tar, copy to a temp file that go-update can consume
		tmpExeFile, err := os.CreateTemp("", "sirserver-extracted-*.tmp")
		if err != nil {
			rc.Close()
			return nil, fmt.Errorf("failed to create temp exe file for zip: %w", err)
		}
		if _, err := io.Copy(tmpExeFile, rc); err != nil {
			rc.Close()
			tmpExeFile.Close()
			os.Remove(tmpExeFile.Name())
			return nil, fmt.Errorf("failed to copy extracted zip entry to temp file: %w", err)
		}
		tmpExeFile.Seek(0, io.SeekStart) // Rewind for go-update
		newBinaryReader = tmpExeFile
		rc.Close() // Close the zip entry reader
		// We defer removal of tmpExeFile inside the defer of the outer function
		defer func() {
			tmpExeFile.Close()
			os.Remove(tmpExeFile.Name())
		}()
	} else {
		// If it's not a known archive, assume it's the raw binary itself.
		// In this case, tmpDownloadedFile already contains the binary.
		tempFileForReading.Seek(0, io.SeekStart) // Ensure it's at the beginning
		newBinaryReader = tempFileForReading
		// The defer for tempFileForReading is already set to close and remove it.
	}

	if newBinaryReader == nil {
		return nil, fmt.Errorf("internal error: new binary reader is nil after download and preparation")
	}

	return newBinaryReader, nil
}
