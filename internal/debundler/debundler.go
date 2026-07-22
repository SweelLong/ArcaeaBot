package debundler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arcaeabot/internal/config"
)

type metadata struct {
	VersionNumber         string       `json:"versionNumber"`
	PreviousVersionNumber *string      `json:"previousVersionNumber"`
	Removed               []string     `json:"removed"`
	Added                 []bundleFile `json:"added"`
}

type bundleFile struct {
	Path       string `json:"path"`
	ByteOffset int64  `json:"byteOffset"`
	Length     int64  `json:"length"`
	Hash       string `json:"sha256HashBase64Encoded"`
}

type versionBundle struct {
	CBPath   string
	MetaPath string
	Metadata metadata
}

func Run(cfg *config.Config) error {
	bundles, err := findBundles(cfg.DebundlerBundlePath)
	if err != nil {
		return err
	}
	if len(bundles) == 0 {
		return fmt.Errorf("no bundle files found in %s; expected matching .cb and .json files", cfg.DebundlerBundlePath)
	}
	if err := prepareOutputDir(cfg.DebundlerOutputPath); err != nil {
		return err
	}
	for _, item := range bundles {
		if err := parseBundle(item, cfg.DebundlerOutputPath, cfg.DebundlerFolders); err != nil {
			return err
		}
	}
	removeIgnoredOutputFiles(cfg.DebundlerOutputPath)
	return nil
}

func prepareOutputDir(outputDir string) error {
	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	cleanupStaleOutputDirs(parent, filepath.Base(outputDir))

	if _, err := os.Stat(outputDir); err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(outputDir, 0o755)
		}
		return err
	}

	oldDir := filepath.Join(parent, fmt.Sprintf(".%s-old-%d", filepath.Base(outputDir), time.Now().UnixNano()))
	if err := os.Rename(outputDir, oldDir); err != nil {
		if err := removeContents(outputDir); err != nil {
			return err
		}
		return os.MkdirAll(outputDir, 0o755)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

func cleanupStaleOutputDirs(parent, base string) {
	pattern := filepath.Join(parent, "."+base+"-old-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, match := range matches {
		_ = os.RemoveAll(match)
	}
}

func removeContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func findBundles(dir string) ([]versionBundle, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return bundleFromCBPath(dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var bundles []versionBundle
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		metaPath := filepath.Join(dir, entry.Name())
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		cbPath := filepath.Join(dir, name+".cb")
		bundle, err := readBundle(cbPath, metaPath)
		if err != nil {
			return nil, err
		}
		bundles = append(bundles, bundle)
	}
	if len(bundles) == 0 {
		return nil, nil
	}
	return orderBundleChain(bundles)
}

func bundleFromCBPath(path string) ([]versionBundle, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".cb") {
		return nil, fmt.Errorf("BUNDLE_PATH must be a directory or .cb file: %s", path)
	}
	bundle, err := makeBundle(path)
	if err != nil {
		return nil, err
	}
	return []versionBundle{bundle}, nil
}

func makeBundle(cbPath string) (versionBundle, error) {
	name := strings.TrimSuffix(filepath.Base(cbPath), filepath.Ext(cbPath))
	metaPath := filepath.Join(filepath.Dir(cbPath), name+".json")
	return readBundle(cbPath, metaPath)
}

func readBundle(cbPath, metaPath string) (versionBundle, error) {
	if _, err := os.Stat(cbPath); err != nil {
		return versionBundle{}, fmt.Errorf("bundle file missing for %s; expected %s", metaPath, cbPath)
	}
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return versionBundle{}, err
	}
	var meta metadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return versionBundle{}, fmt.Errorf("metadata %s is not valid JSON: %w", metaPath, err)
	}
	if meta.VersionNumber == "" {
		return versionBundle{}, fmt.Errorf("metadata %s has no versionNumber", metaPath)
	}
	return versionBundle{
		CBPath:   cbPath,
		MetaPath: metaPath,
		Metadata: meta,
	}, nil
}

func orderBundleChain(bundles []versionBundle) ([]versionBundle, error) {
	var first *versionBundle
	next := make(map[string]*versionBundle, len(bundles))
	versions := make(map[string]struct{}, len(bundles))
	for i := range bundles {
		bundle := &bundles[i]
		version := bundle.Metadata.VersionNumber
		if _, exists := versions[version]; exists {
			return nil, fmt.Errorf("duplicate bundle versionNumber %q", version)
		}
		versions[version] = struct{}{}
		if bundle.Metadata.PreviousVersionNumber == nil {
			if first != nil {
				return nil, fmt.Errorf("multiple bundle chain roots: %s and %s", first.MetaPath, bundle.MetaPath)
			}
			first = bundle
			continue
		}
		previous := *bundle.Metadata.PreviousVersionNumber
		if existing := next[previous]; existing != nil {
			return nil, fmt.Errorf("multiple bundles follow versionNumber %q", previous)
		}
		next[previous] = bundle
	}
	if first == nil {
		return nil, fmt.Errorf("bundle chain has no previousVersionNumber null root")
	}
	ordered := make([]versionBundle, 0, len(bundles))
	seen := make(map[string]struct{}, len(bundles))
	for current := first; current != nil; current = next[current.Metadata.VersionNumber] {
		version := current.Metadata.VersionNumber
		if _, exists := seen[version]; exists {
			return nil, fmt.Errorf("bundle chain loops at versionNumber %q", version)
		}
		seen[version] = struct{}{}
		ordered = append(ordered, *current)
	}
	if len(ordered) != len(bundles) {
		return nil, fmt.Errorf("bundle chain contains %d linked metadata files out of %d", len(ordered), len(bundles))
	}
	return ordered, nil
}

func parseBundle(item versionBundle, outputDir string, folders []string) error {
	cb, err := os.Open(item.CBPath)
	if err != nil {
		return err
	}
	defer cb.Close()
	for _, path := range item.Metadata.Removed {
		if isIgnoredPath(path) || !shouldExtract(path, folders) {
			continue
		}
		outPath, err := bundleOutputPath(outputDir, path)
		if err != nil {
			return err
		}
		if err := os.Remove(outPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, file := range item.Metadata.Added {
		if isIgnoredPath(file.Path) {
			continue
		}
		if !shouldExtract(file.Path, folders) {
			continue
		}
		if err := extractFile(cb, outputDir, file); err != nil {
			return err
		}
	}
	return nil
}

func shouldExtract(path string, folders []string) bool {
	if len(folders) == 0 {
		return true
	}
	path = strings.Trim(filepath.ToSlash(path), "/")
	for _, folder := range folders {
		folder = strings.Trim(filepath.ToSlash(folder), "/")
		if folder == "" {
			continue
		}
		if path == folder || strings.HasPrefix(path, folder+"/") {
			return true
		}
	}
	return false
}

func isIgnoredPath(path string) bool {
	path = strings.Trim(filepath.ToSlash(path), "/")
	for _, part := range strings.Split(path, "/") {
		switch part {
		case ".DS_Store", "Thumbs.db":
			return true
		}
	}
	return false
}

func removeIgnoredOutputFiles(outputDir string) {
	for _, name := range []string{".DS_Store", "Thumbs.db"} {
		_ = os.Remove(filepath.Join(outputDir, name))
	}
}

func extractFile(cb *os.File, outputDir string, file bundleFile) error {
	expected, err := base64.StdEncoding.DecodeString(file.Hash)
	if err != nil {
		return err
	}
	if _, err := cb.Seek(file.ByteOffset, io.SeekStart); err != nil {
		return err
	}
	data := make([]byte, file.Length)
	if _, err := io.ReadFull(cb, data); err != nil {
		return err
	}
	actual := sha256.Sum256(data)
	if string(actual[:]) != string(expected) {
		return fmt.Errorf("file hash mismatch for %s", file.Path)
	}
	outPath, err := bundleOutputPath(outputDir, file.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, data, 0o644)
}

func bundleOutputPath(outputDir, path string) (string, error) {
	path = filepath.Clean(filepath.FromSlash(strings.TrimPrefix(path, "/")))
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle path escapes output directory: %s", path)
	}
	return filepath.Join(outputDir, path), nil
}
