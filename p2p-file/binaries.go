package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type binaryArtifact struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	File string `json:"file"`
	Size int64  `json:"size"`
	path string
}

func loadBinaryArtifacts(baseDir string) []binaryArtifact {
	if baseDir == "" {
		return nil
	}
	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	var artifacts []binaryArtifact
	_ = filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		osName, arch, ok := parseArtifactName(entry.Name())
		if !ok {
			return nil
		}
		fi, err := entry.Info()
		if err != nil {
			return nil
		}
		artifacts = append(artifacts, binaryArtifact{
			OS:   osName,
			Arch: arch,
			File: entry.Name(),
			Size: fi.Size(),
			path: path,
		})
		return nil
	})
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].OS == artifacts[j].OS {
			if artifacts[i].Arch == artifacts[j].Arch {
				return artifacts[i].File < artifacts[j].File
			}
			return artifacts[i].Arch < artifacts[j].Arch
		}
		return artifacts[i].OS < artifacts[j].OS
	})
	return artifacts
}

func parseArtifactName(name string) (string, string, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, "_")
	if len(parts) < 3 {
		return "", "", false
	}
	osName := parts[len(parts)-2]
	arch := parts[len(parts)-1]
	if osName == "" || arch == "" {
		return "", "", false
	}
	return osName, arch, true
}

func detectPlatformFromUA(ua string) (string, string) {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "windows"):
		osName = "windows"
	case strings.Contains(ua, "mac os x"), strings.Contains(ua, "macintosh"), strings.Contains(ua, "darwin"):
		osName = "darwin"
	case strings.Contains(ua, "linux"):
		osName = "linux"
	}

	switch {
	case strings.Contains(ua, "arm64"), strings.Contains(ua, "aarch64"):
		arch = "arm64"
	case strings.Contains(ua, "x86_64"), strings.Contains(ua, "win64"), strings.Contains(ua, "wow64"), strings.Contains(ua, "amd64"):
		arch = "amd64"
	}
	return osName, arch
}

func (a *app) findBinary(osName, arch string) (*binaryArtifact, bool) {
	for i := range a.binaries {
		if strings.EqualFold(a.binaries[i].OS, osName) && strings.EqualFold(a.binaries[i].Arch, arch) {
			return &a.binaries[i], true
		}
	}
	return nil, false
}
