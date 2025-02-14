package internal

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/reinhrst/fzf-lib" // <-- added for fuzzy search
	"github.com/shirou/gopsutil/disk"
	variable "github.com/yorukot/superfile/src/config"
	"github.com/yorukot/superfile/src/config/icon"
)

// Return all sidebar directories
func getDirectories() []directory {
	directories := []directory{}

	directories = append(directories, getWellKnownDirectories()...)
	directories = append(directories, directory{
		// Just make sure no one owns the hard drive or directory named this path
		location: "Pinned+-*/=?",
	})
	directories = append(directories, getPinnedDirectories()...)
	directories = append(directories, directory{
		// Just make sure no one owns the hard drive or directory named this path
		location: "Disks+-*/=?",
	})
	directories = append(directories, getExternalMediaFolders()...)
	return directories
}

// Return system default directory e.g. Home, Downloads, etc
func getWellKnownDirectories() []directory {
	directories := []directory{}
	wellKnownDirectories := []directory{
		{location: xdg.Home, name: icon.Home + icon.Space + "Home"},
		{location: xdg.UserDirs.Download, name: icon.Download + icon.Space + "Downloads"},
		{location: xdg.UserDirs.Documents, name: icon.Documents + icon.Space + "Documents"},
		{location: xdg.UserDirs.Pictures, name: icon.Pictures + icon.Space + "Pictures"},
		{location: xdg.UserDirs.Videos, name: icon.Videos + icon.Space + "Videos"},
		{location: xdg.UserDirs.Music, name: icon.Music + icon.Space + "Music"},
		{location: xdg.UserDirs.Templates, name: icon.Templates + icon.Space + "Templates"},
		{location: xdg.UserDirs.PublicShare, name: icon.PublicShare + icon.Space + "PublicShare"},
	}

	for _, dir := range wellKnownDirectories {
		if _, err := os.Stat(dir.location); !os.IsNotExist(err) {
			// Directory exists
			directories = append(directories, dir)
		}
	}

	return directories
}

// Get user pinned directories
func getPinnedDirectories() []directory {
	directories := []directory{}
	var paths []string
	var pinnedDirs []struct {
		Location string `json:"location"`
		Name     string `json:"name"`
	}

	jsonData, err := os.ReadFile(variable.PinnedFile)
	if err != nil {
		outPutLog("Read superfile data error", err)
		return directories
	}

	// Check if the data is in the old format
	if err := json.Unmarshal(jsonData, &paths); err == nil {
		for _, path := range paths {
			directoryName := filepath.Base(path)
			directories = append(directories, directory{location: path, name: directoryName})
		}
		// Check if the data is in the new format
	} else if err := json.Unmarshal(jsonData, &pinnedDirs); err == nil {
		for _, pinnedDir := range pinnedDirs {
			directories = append(directories, directory{location: pinnedDir.Location, name: pinnedDir.Name})
		}
		// If the data is in neither format, log the error
	} else {
		outPutLog("Error parsing pinned data", err)
	}
	return directories
}

// Get external media directories
func getExternalMediaFolders() (disks []directory) {
	parts, err := disk.Partitions(true)

	if err != nil {
		outPutLog("Error while getting external media: ", err)
	}
	for _, disk := range parts {
		if isExternalDiskPath(disk.Mountpoint) {
			disks = append(disks, directory{
				name:     filepath.Base(disk.Mountpoint),
				location: disk.Mountpoint,
			})
		}
	}
	if err != nil {
		outPutLog("Error while getting external media: ", err)
	}
	return disks
}

// Fuzzy search function for a list of directories.
func fuzzySearch(query string, dirs []directory) []directory {
	var filteredDirs []directory
	if len(dirs) > 0 {
		haystack := make([]string, len(dirs))
		dirMap := make(map[string]directory, len(dirs))

		for i, dir := range dirs {
			haystack[i] = dir.name
			dirMap[dir.name] = dir
		}

		options := fzf.DefaultOptions()
		fzfNone := fzf.New(haystack, options)
		fzfNone.Search(query)
		result := <-fzfNone.GetResultChannel()
		fzfNone.End()

		for _, match := range result.Matches {
			if d, ok := dirMap[match.Key]; ok {
				filteredDirs = append(filteredDirs, d)
			}
		}
	}
	return filteredDirs
}

// Get filtered directories using fuzzy search logic with three haystacks.
func getFilteredDirectories(query string) []directory {
	// Get all directories.
	allDirs := getDirectories()

	var noneDirs []directory
	var pinnedDirs []directory
	var diskDirs []directory

	// Partition directories into three groups.
	var currentGroup *[]directory
	for _, dir := range allDirs {
		switch dir.location {
		case "Pinned+-*/=?":
			currentGroup = &pinnedDirs
		case "Disks+-*/=?":
			currentGroup = &diskDirs
		default:
			if currentGroup != nil {
				*currentGroup = append(*currentGroup, dir)
			} else {
				noneDirs = append(noneDirs, dir)
			}
		}
	}

	// Run fuzzy search on each group.
	filteredNone := fuzzySearch(query, noneDirs)
	filteredPinned := fuzzySearch(query, pinnedDirs)
	filteredDisks := fuzzySearch(query, diskDirs)

	// Combine fuzzy-matched directories.
	var filteredDirs []directory
	filteredDirs = append(filteredDirs, filteredNone...)
	filteredDirs = append(filteredDirs, directory{
		location: "Pinned+-*/=?",
	})
	filteredDirs = append(filteredDirs, filteredPinned...)
	filteredDirs = append(filteredDirs, directory{
		location: "Disks+-*/=?",
	})
	filteredDirs = append(filteredDirs, filteredDisks...)

	return filteredDirs
}
