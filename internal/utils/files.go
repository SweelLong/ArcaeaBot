package utils

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func ImageFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".gif") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files
}

func MaxImageNumber(dir string) int {
	maxN := 0
	for _, file := range ImageFiles(dir) {
		if n, _ := strconv.Atoi(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))); n > maxN {
			maxN = n
		}
	}
	return maxN
}

func LastInt(s string) int {
	fields := strings.Fields(s)
	for i := len(fields) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(fields[i]); err == nil {
			return n
		}
	}
	return 0
}
