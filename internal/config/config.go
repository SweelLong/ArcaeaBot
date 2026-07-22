package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"arcaeabot/internal/database"
	"gopkg.in/yaml.v3"
)

type Config struct {
	WSURL          string   `yaml:"ws_url"`
	WSToken        string   `yaml:"ws_token"`
	QQ             int64    `yaml:"bot_qq"`
	ThreadCount    int      `yaml:"thread_count"`
	EnabledPlugins []string `yaml:"enabled_plugins"`
	PublicCommands []string `yaml:"public_commands"`

	ArcaeaDatabaseURL    string `yaml:"arcaea_database_url"`
	ArcaeaLogDatabaseURL string `yaml:"arcaea_log_database_url"`

	BundleSongsPath     string   `yaml:"-"`
	BundleCharPath      string   `yaml:"-"`
	SonglistPath        string   `yaml:"-"`
	DebundlerBundlePath string   `yaml:"bundle_path"`
	DebundlerOutputPath string   `yaml:"-"`
	DebundlerFolders    []string `yaml:"debundler_folders"`

	ResourcesPath string `yaml:"resources_path"`
	DataPath      string `yaml:"data_path"`
	TmpPath       string `yaml:"tmp_path"`

	BotName                  string          `yaml:"bot_name"`
	GameName                 string          `yaml:"game_name"`
	TicketName               string          `yaml:"ticket_name"`
	FragName                 string          `yaml:"frag_name"`
	RenameCost               int             `yaml:"arcaea_rename_cost"`
	DefaultTicket            int             `yaml:"arcaea_default_ticket"`
	GroupWhitelist           []int64         `yaml:"group_whitelist"`
	CheckInRewards           []CheckInReward `yaml:"check_in_rewards"`
	CheckInExpireHours       int             `yaml:"check_in_expire_hours"`
	FragmentExpireDays       int             `yaml:"fragment_present_expire_days"`
	SnatchEnableTicket       int             `yaml:"snatch_enable_ticket"`
	LLMID                    string          `yaml:"llm_id"`
	LLMURL                   string          `yaml:"llm_url"`
	LLMAPIKey                string          `yaml:"llm_api_key"`
	MaxChatCount             int             `yaml:"max_chat_count"`
	HashRankLimit            int             `yaml:"hash_rank_limit"`
	PTTRankLimit             int             `yaml:"ptt_rank_limit"`
	GlobalPlayerRankLimit    int             `yaml:"global_player_rank_limit"`
	ReportTypes              []string        `yaml:"report_types"`
	HelpTips                 []string        `yaml:"help_tips"`
	TarotCards               []TarotCard     `yaml:"tarot_cards"`
	StickerMirrorProbability *float64        `yaml:"sticker_mirror_probability"`
}

type CheckInReward struct {
	ItemID string `yaml:"item_id"`
	Type   string `yaml:"type"`
	Name   string `yaml:"name"`
	Amount int    `yaml:"amount"`
}

type TarotCard struct {
	Name     string `yaml:"name"`
	Effect   string `yaml:"effect"`
	Duration int    `yaml:"duration"`
}

func Load(path string) (*Config, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := loadYAML(path, cfg); err != nil {
		return nil, err
	}
	if err := cfg.normalize(root); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadYAML(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *Config) normalize(root string) error {
	if strings.TrimSpace(c.FragName) == "" {
		c.FragName = "合成玉"
	}
	if c.StickerMirrorProbability == nil {
		defaultProbability := 0.5
		c.StickerMirrorProbability = &defaultProbability
	}
	if *c.StickerMirrorProbability < 0 {
		*c.StickerMirrorProbability = 0
	}
	if *c.StickerMirrorProbability > 1 {
		*c.StickerMirrorProbability = 1
	}
	if c.ThreadCount < 1 {
		return errors.New("thread_count must be greater than zero")
	}
	_, arcDSN, err := database.ParseConnectionURL(c.ArcaeaDatabaseURL)
	if err != nil {
		return fmt.Errorf("arcaea_database_url: %w", err)
	}
	_, arcLogDSN, err := database.ParseConnectionURL(c.ArcaeaLogDatabaseURL)
	if err != nil {
		return fmt.Errorf("arcaea_log_database_url: %w", err)
	}
	c.DataPath = abs(root, c.DataPath)
	c.ResourcesPath = abs(root, c.ResourcesPath)
	c.TmpPath = abs(root, c.TmpPath)
	arcDSN = absSQLiteDSN(root, arcDSN)
	c.ArcaeaDatabaseURL = "sqlite://" + arcDSN
	arcLogDSN = absSQLiteDSN(root, arcLogDSN)
	c.ArcaeaLogDatabaseURL = "sqlite://" + arcLogDSN
	c.DebundlerBundlePath = abs(root, c.DebundlerBundlePath)
	c.DebundlerFolders = normalizeBundleFolders(c.DebundlerFolders)
	c.DebundlerOutputPath = filepath.Join(c.TmpPath, "debundler")
	c.BundleSongsPath = filepath.Join(c.DebundlerOutputPath, "songs")
	c.BundleCharPath = filepath.Join(c.DebundlerOutputPath, "char")
	c.SonglistPath = filepath.Join(c.BundleSongsPath, "songlist")
	for _, path := range []string{c.DataPath, c.TmpPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create path %s: %w", path, err)
		}
	}
	if path := sqliteFilePath(arcDSN); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create database path: %w", err)
		}
	}
	if path := sqliteFilePath(arcLogDSN); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create log database path: %w", err)
		}
	}
	return nil
}

func (c *Config) PluginEnabled(name string) bool {
	if len(c.EnabledPlugins) == 0 {
		return true
	}
	for _, enabled := range c.EnabledPlugins {
		if enabled == "*" || strings.EqualFold(enabled, name) {
			return true
		}
	}
	return false
}

func abs(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(root, path))
}

func absSQLiteDSN(root, dsn string) string {
	if dsn == "" || dsn == ":memory:" || strings.HasPrefix(dsn, "file:") {
		return dsn
	}
	return abs(root, dsn)
}

func sqliteFilePath(dsn string) string {
	if dsn == "" || dsn == ":memory:" || strings.HasPrefix(dsn, "file:") {
		return ""
	}
	return dsn
}

func normalizeBundleFolders(folders []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(folders))
	for _, folder := range folders {
		folder = strings.Trim(filepath.ToSlash(strings.TrimSpace(folder)), "/")
		if folder == "" {
			continue
		}
		if _, ok := seen[folder]; ok {
			continue
		}
		seen[folder] = struct{}{}
		out = append(out, folder)
	}
	return out
}
