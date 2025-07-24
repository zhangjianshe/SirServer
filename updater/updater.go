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
	color.Yellow("Checking for updates...")

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
	downloadURL := u.DownloadBaseURL + "/" + updateInfo.LatestVersion + "/" + targetDownload.Filename
	color.Yellow("Downloading update from: %s", downloadURL)

	// 6. Download the archive/binary
	newBinaryReader, err := u.downloadAndPrepareBinary(downloadURL, targetDownload.Filename)
	if err != nil {
		color.Red("failed to download and prepare new binary:%s", err.Error())
		return fmt.Errorf("failed to download and prepare new binary: %w", err)
	}
	defer func() { // This defer needs to be conditional and correctly applied
		// If newBinaryReader is an io.ReadCloser, ensure it's closed
		if closer, ok := newBinaryReader.(io.ReadCloser); ok {
			closer.Close()
		}
	}()

	// 7. Apply the update using go-update
	color.Yellow("Applying update...")
	err = update.Apply(newBinaryReader, update.Options{})
	if err != nil {
		color.Red("failed to apply update: %s", err.Error())
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

// downloadAndPrepareBinary downloads the specified file and returns an io.ReadCloser for the extracted executable.
// The caller is responsible for closing the returned io.ReadCloser.
func (u *Updater) downloadAndPrepareBinary(url, filename string) (io.ReadCloser, error) { // <--- Change return type to io.ReadCloser
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file, HTTP status %s", resp.Status)
	}

	tmpDownloadedFile, err := os.CreateTemp("", "sirserver-update-download-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary download file: %w", err)
	}
	// DO NOT DEFER tmpDownloadedFile.Close() or os.Remove(tmpDownloadedFile.Name()) here for the *downloaded* file.
	// We need to keep it open or ensure it's removed after being consumed by the zip/tar reader, or passed as raw binary.

	_, err = io.Copy(tmpDownloadedFile, resp.Body)
	if err != nil {
		tmpDownloadedFile.Close()
		os.Remove(tmpDownloadedFile.Name())
		return nil, fmt.Errorf("failed to write downloaded content to temporary file: %w", err)
	}
	tmpDownloadedFile.Close() // Close after writing all content. The file handle is closed, but the file on disk remains.

	// Now open the downloaded temporary file for reading and decompression/extraction
	tempFileForReading, err := os.Open(tmpDownloadedFile.Name())
	if err != nil {
		os.Remove(tmpDownloadedFile.Name()) // Clean up original downloaded file if we can't open it
		return nil, fmt.Errorf("failed to open temporary downloaded file for reading: %w", err)
	}
	// NO DEFER tmpFileForReading.Close() HERE! The caller will close the returned reader.
	// We will still remove tmpDownloadedFile.Name() at the end, as it's just the archive.

	var newBinaryReader io.ReadCloser // <--- Change type here

	executableName := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		executableName = strings.TrimSuffix(executableName, ".exe")
	}

	if strings.HasSuffix(filename, ".tar.gz") {
		color.Yellow("Decompressing .tar.gz archive...")
		gzr, err := gzip.NewReader(tempFileForReading) // Read from the temp file
		if err != nil {
			tempFileForReading.Close()          // Ensure this is closed if gzip fails
			os.Remove(tmpDownloadedFile.Name()) // Clean up the downloaded file
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzr.Close() // Close gzip reader as soon as tar reader is done

		tr := tar.NewReader(gzr)
		var found bool
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				tempFileForReading.Close()          // Ensure this is closed
				os.Remove(tmpDownloadedFile.Name()) // Clean up
				return nil, fmt.Errorf("failed to read tar header: %w", err)
			}

			if header.Typeflag == tar.TypeReg && strings.TrimSuffix(filepath.Base(header.Name), ".exe") == executableName {
				tmpExeFile, err := os.CreateTemp("", "sirserver-extracted-*.tmp")
				if err != nil {
					tempFileForReading.Close()          // Ensure this is closed
					os.Remove(tmpDownloadedFile.Name()) // Clean up
					return nil, fmt.Errorf("failed to create temp exe file for tar: %w", err)
				}
				if _, err := io.Copy(tmpExeFile, tr); err != nil {
					tmpExeFile.Close()
					os.Remove(tmpExeFile.Name())
					tempFileForReading.Close()          // Ensure this is closed
					os.Remove(tmpDownloadedFile.Name()) // Clean up
					return nil, fmt.Errorf("failed to copy extracted tar entry to temp file: %w", err)
				}
				tmpExeFile.Seek(0, io.SeekStart)
				newBinaryReader = tmpExeFile // This is the file that will be returned
				found = true
				break
			}
		}
		if !found {
			tempFileForReading.Close()          // Ensure this is closed
			os.Remove(tmpDownloadedFile.Name()) // Clean up
			return nil, fmt.Errorf("could not find executable (%s) inside .tar.gz archive", executableName)
		}
	} else if strings.HasSuffix(filename, ".zip") {
		color.Yellow("Decompressing .zip archive...")
		zipReader, err := zip.OpenReader(tmpDownloadedFile.Name())
		if err != nil {
			tempFileForReading.Close()          // Ensure this is closed
			os.Remove(tmpDownloadedFile.Name()) // Clean up
			return nil, fmt.Errorf("failed to open zip file: %w", err)
		}
		defer zipReader.Close()

		var exeFile *zip.File
		for _, f := range zipReader.File {
			if !f.FileInfo().IsDir() && strings.TrimSuffix(filepath.Base(f.Name), ".exe") == executableName {
				exeFile = f
				break
			}
		}
		if exeFile == nil {
			tempFileForReading.Close()          // Ensure this is closed
			os.Remove(tmpDownloadedFile.Name()) // Clean up
			return nil, fmt.Errorf("could not find executable (%s) inside .zip archive", executableName)
		}

		rc, err := exeFile.Open()
		if err != nil {
			tempFileForReading.Close()          // Ensure this is closed
			os.Remove(tmpDownloadedFile.Name()) // Clean up
			return nil, fmt.Errorf("failed to open executable in zip: %w", err)
		}
		defer rc.Close() // Close the zip entry reader

		tmpExeFile, err := os.CreateTemp("", "sirserver-extracted-*.tmp")
		if err != nil {
			tempFileForReading.Close()          // Ensure this is closed
			os.Remove(tmpDownloadedFile.Name()) // Clean up
			return nil, fmt.Errorf("failed to create temp exe file for zip: %w", err)
		}
		if _, err := io.Copy(tmpExeFile, rc); err != nil {
			tmpExeFile.Close()
			os.Remove(tmpExeFile.Name())
			tempFileForReading.Close()          // Ensure this is closed
			os.Remove(tmpDownloadedFile.Name()) // Clean up
			return nil, fmt.Errorf("failed to copy extracted zip entry to temp file: %w", err)
		}
		tmpExeFile.Seek(0, io.SeekStart)
		newBinaryReader = tmpExeFile // This is the file that will be returned
	} else {
		// If it's not a known archive, assume it's the raw binary itself.
		// In this case, tmpDownloadedFile already contains the binary.
		tempFileForReading.Seek(0, io.SeekStart)
		newBinaryReader = tempFileForReading // This is the file that will be returned
	}

	if newBinaryReader == nil {
		tempFileForReading.Close()          // Ensure this is closed
		os.Remove(tmpDownloadedFile.Name()) // Clean up
		return nil, fmt.Errorf("internal error: new binary reader is nil after download and preparation")
	}

	// Now, clean up the original downloaded archive file here.
	// We only need the extracted executable (newBinaryReader).
	// The tempFileForReading was opened on tmpDownloadedFile.Name(), so we remove that.
	os.Remove(tmpDownloadedFile.Name())

	return newBinaryReader, nil
}
