package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	gopisecrets "github.com/cgund98/gopi/internal/secrets"
)

const (
	dirName          = ".gopi"
	configFileName   = "config.toml"
	systemPromptFile = "system.md"
	defaultModel     = "gpt-6-sol"
	defaultMaxIter   = 10
	homeDirEnv       = "GOPI_HOME"
	requiredDirMode  = os.FileMode(0o700)
)

// File is the on-disk ~/.gopi/config.toml shape.
// [sandbox.network] is rewritten to [sandbox.hosts] before decode, because TOML
// cannot store both network = "deny" and a [sandbox.network] table.
type File struct {
	Model         string           `toml:"model"`
	MaxIterations int              `toml:"max_iterations"`
	Sandbox       SandboxFile      `toml:"sandbox"`
	Instructions  InstructionsFile `toml:"instructions"`
}

// SandboxFile is the [sandbox] table.
type SandboxFile struct {
	Network string    `toml:"network"`
	Hosts   HostsFile `toml:"hosts"`
}

// InstructionsFile is the [instructions] table.
type InstructionsFile struct {
	ProjectDocMaxBytes int      `toml:"project_doc_max_bytes"`
	FallbackFiles      []string `toml:"fallback_files"`
	SkillDirs          []string `toml:"skill_dirs"`
}
type HostsFile struct {
	Allow []string `toml:"allow"`
	Deny  []string `toml:"deny"`
}

// Config is the process configuration for one gopi run.
type Config struct {
	Model              string
	MaxIterations      int
	SystemPrompt       string
	OpenAIAPIKey       string
	HomeDir            string
	Secrets            map[string]string
	Network            string
	AllowHosts         []string
	DenyHosts          []string
	UserPrompt         string
	ProjectDocMaxBytes int
	FallbackFiles      []string
	SkillDirs          []string
}

type envSecrets struct {
	OpenAIAPIKey string `envconfig:"OPENAI_API_KEY"`
}

// HomeDir returns the gopi configuration directory.
// GOPI_HOME overrides the default ~/.gopi path.
func HomeDir() (string, error) {
	if override := os.Getenv(homeDirEnv); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// EnsureHome creates the config directory at mode 0700.
// An existing directory that is group- or world-accessible is refused.
func EnsureHome(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(dir, requiredDirMode); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
			return nil
		}
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		return fmt.Errorf("%s is mode %o; refuse to start unless it is 0700", dir, perm)
	}
	return nil
}

// Load reads config.toml from dir, creating a default file when missing.
// system.md is returned as UserPrompt and does not replace the built-in prompt.
// OPENAI_API_KEY is read from the environment after optional .env files.
func Load(dir string) (Config, error) {
	if err := EnsureHome(dir); err != nil {
		return Config{}, err
	}

	path := filepath.Join(dir, configFileName)
	file, err := loadOrCreateFile(path)
	if err != nil {
		return Config{}, err
	}
	if file.Sandbox.Network == "" {
		file.Sandbox.Network = "deny"
	}
	if file.Model == "" {
		file.Model = defaultModel
	}
	if file.MaxIterations <= 0 {
		file.MaxIterations = defaultMaxIter
	}
	if file.Instructions.ProjectDocMaxBytes <= 0 {
		file.Instructions.ProjectDocMaxBytes = 32768
	}

	var userPrompt string
	systemPath := filepath.Join(dir, systemPromptFile)
	if body, err := os.ReadFile(systemPath); err == nil {
		userPrompt = string(body)
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read system prompt: %w", err)
	}

	secrets, err := loadSecrets()
	if err != nil {
		return Config{}, err
	}

	broker, err := gopisecrets.Load(dir)
	if err != nil {
		return Config{}, err
	}
	apiKey := secrets.OpenAIAPIKey
	if broker["openai_api_key"] != "" {
		apiKey = broker["openai_api_key"]
	}

	return Config{
		Model:              file.Model,
		MaxIterations:      file.MaxIterations,
		SystemPrompt:       "",
		OpenAIAPIKey:       apiKey,
		HomeDir:            dir,
		Secrets:            broker,
		Network:            file.Sandbox.Network,
		AllowHosts:         file.Sandbox.Hosts.Allow,
		DenyHosts:          file.Sandbox.Hosts.Deny,
		UserPrompt:         userPrompt,
		ProjectDocMaxBytes: file.Instructions.ProjectDocMaxBytes,
		FallbackFiles:      file.Instructions.FallbackFiles,
		SkillDirs:          file.Instructions.SkillDirs,
	}, nil
}

func loadOrCreateFile(path string) (File, error) {
	var file File
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return File{}, fmt.Errorf("stat config: %w", err)
		}
		file = File{Model: defaultModel, MaxIterations: defaultMaxIter}
		encoded, err := toml.Marshal(file)
		if err != nil {
			return File{}, fmt.Errorf("encode default config: %w", err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			return File{}, fmt.Errorf("write default config: %w", err)
		}
		return file, nil
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}
	return decodeFile(body)
}

func decodeFile(body []byte) (File, error) {
	text := strings.ReplaceAll(string(body), "[sandbox.network]", "[sandbox.hosts]")
	var file File
	if err := toml.Unmarshal([]byte(text), &file); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}
	if file.Sandbox.Network == "" {
		file.Sandbox.Network = "deny"
	}
	switch file.Sandbox.Network {
	case "deny", "allowlist":
	default:
		return File{}, fmt.Errorf("sandbox network %q is not available; use deny or allowlist", file.Sandbox.Network)
	}
	return file, nil
}

func loadSecrets() (envSecrets, error) {
	if _, err := os.Stat(".env.local"); err == nil {
		if err := godotenv.Load(".env.local"); err != nil {
			return envSecrets{}, fmt.Errorf("load .env.local: %w", err)
		}
	}
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			return envSecrets{}, fmt.Errorf("load .env: %w", err)
		}
	}

	var secrets envSecrets
	if err := envconfig.Process("", &secrets); err != nil {
		return envSecrets{}, fmt.Errorf("read environment: %w", err)
	}
	return secrets, nil
}
