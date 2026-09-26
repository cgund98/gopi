package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	"github.com/cgund98/gopi/internal/models"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
)

const (
	dirName          = ".gopi"
	configFileName   = "config.toml"
	systemPromptFile = "system.md"
	defaultModel     = "gpt-5.6-terra"
	defaultMaxIter   = 10
	homeDirEnv       = "GOPI_HOME"
	requiredDirMode  = os.FileMode(0o700)
)

// File is the on-disk ~/.gopi/config.toml shape.
// [sandbox.network] is rewritten to [sandbox.hosts] before decode, because TOML
// cannot store both network = "deny" and a [sandbox.network] table.
type File struct {
	Model         string           `toml:"model"`
	Models        ModelsFile       `toml:"models"`
	MaxIterations int              `toml:"max_iterations"`
	Sandbox       SandboxFile      `toml:"sandbox"`
	Instructions  InstructionsFile `toml:"instructions"`
	Search        SearchFile       `toml:"search"`
}

// ModelsFile is the optional [models] table. Empty keys fall back to model.
type ModelsFile struct {
	Agent string `toml:"agent"`
	Ask   string `toml:"ask"`
	Plan  string `toml:"plan"`
	Build string `toml:"build"`
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

// SearchFile is the [search] table.
type SearchFile struct {
	Endpoint string `toml:"endpoint"`
}

// Config is the process configuration for one gopi run.
type Config struct {
	Model              string
	AgentModel         string
	AskModel           string
	PlanModel          string
	BuildModel         string
	MaxIterations      int
	SystemPrompt       string
	OpenAIAPIKey       string
	KimiAPIKey         string
	HomeDir            string
	Secrets            map[string]string
	SecretFiles        map[string]string
	HostOnly           []string
	Network            string
	AllowHosts         []string
	DenyHosts          []string
	UserPrompt         string
	ProjectDocMaxBytes int
	FallbackFiles      []string
	SkillDirs          []string
	SearchEndpoint     string
}

type envSecrets struct {
	OpenAIAPIKey string `envconfig:"OPENAI_API_KEY"`
	KimiAPIKey   string `envconfig:"KIMI_API_KEY"`
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

	broker, secretFiles, err := gopisecrets.Load(dir)
	if err != nil {
		return Config{}, err
	}
	apiKey := secrets.OpenAIAPIKey
	if broker[gopisecrets.OpenAIAPIKey] != "" {
		apiKey = broker[gopisecrets.OpenAIAPIKey]
	}
	kimiKey := secrets.KimiAPIKey
	if broker[gopisecrets.KimiAPIKey] != "" {
		kimiKey = broker[gopisecrets.KimiAPIKey]
	}
	resolved, err := resolveModels(file)
	if err != nil {
		return Config{}, err
	}
	if err := requireKeys(resolved, apiKey, kimiKey); err != nil {
		return Config{}, err
	}
	endpoint := strings.TrimSpace(file.Search.Endpoint)
	if endpoint == "" {
		endpoint = "https://api.search.brave.com/res/v1/web/search"
	}

	return Config{
		Model:              resolved.fallback,
		AgentModel:         resolved.agent,
		AskModel:           resolved.ask,
		PlanModel:          resolved.plan,
		BuildModel:         resolved.build,
		MaxIterations:      file.MaxIterations,
		SystemPrompt:       "",
		OpenAIAPIKey:       apiKey,
		KimiAPIKey:         kimiKey,
		HomeDir:            dir,
		Secrets:            broker,
		SecretFiles:        secretFiles,
		Network:            file.Sandbox.Network,
		AllowHosts:         file.Sandbox.Hosts.Allow,
		DenyHosts:          file.Sandbox.Hosts.Deny,
		UserPrompt:         userPrompt,
		ProjectDocMaxBytes: file.Instructions.ProjectDocMaxBytes,
		FallbackFiles:      file.Instructions.FallbackFiles,
		SkillDirs:          file.Instructions.SkillDirs,
		SearchEndpoint:     endpoint,
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

type resolvedModels struct {
	fallback string
	agent    string
	ask      string
	plan     string
	build    string
}

func resolveModels(file File) (resolvedModels, error) {
	fallback := strings.TrimSpace(file.Model)
	if fallback == "" {
		fallback = defaultModel
	}
	if _, _, err := models.Parse(fallback); err != nil {
		return resolvedModels{}, err
	}
	agent, err := pickModel(file.Models.Agent, fallback)
	if err != nil {
		return resolvedModels{}, err
	}
	ask, err := pickModel(file.Models.Ask, fallback)
	if err != nil {
		return resolvedModels{}, err
	}
	plan, err := pickModel(file.Models.Plan, fallback)
	if err != nil {
		return resolvedModels{}, err
	}
	build := ""
	if strings.TrimSpace(file.Models.Build) != "" {
		build, err = pickModel(file.Models.Build, fallback)
		if err != nil {
			return resolvedModels{}, err
		}
	}
	return resolvedModels{fallback: fallback, agent: agent, ask: ask, plan: plan, build: build}, nil
}

func pickModel(override, fallback string) (string, error) {
	name := strings.TrimSpace(override)
	if name == "" {
		name = fallback
	}
	if _, _, err := models.Parse(name); err != nil {
		return "", err
	}
	return name, nil
}

func requireKeys(resolved resolvedModels, openaiKey, kimiKey string) error {
	names := []string{resolved.fallback, resolved.agent, resolved.ask, resolved.plan, resolved.build}
	var needOpenAI, needKimi bool
	for _, name := range names {
		if name == "" {
			continue
		}
		provider, _, err := models.Parse(name)
		if err != nil {
			return err
		}
		switch provider {
		case models.ProviderOpenAI:
			needOpenAI = true
		case models.ProviderKimi:
			needKimi = true
		}
	}
	if needOpenAI && openaiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}
	if needKimi && kimiKey == "" {
		return fmt.Errorf("%s is required", gopisecrets.KimiAPIKey)
	}
	return nil
}

// ModelFor returns the resolved model for agent, ask, or plan.
func (c Config) ModelFor(mode string) string {
	var override string
	switch mode {
	case "agent":
		override = c.AgentModel
	case "ask":
		override = c.AskModel
	case "plan":
		override = c.PlanModel
	}
	if override != "" {
		return override
	}
	if c.Model != "" {
		return c.Model
	}
	return defaultModel
}

// BuildModelName returns the model used by the plan build key.
func (c Config) BuildModelName() string {
	if c.BuildModel != "" {
		return c.BuildModel
	}
	return c.ModelFor("agent")
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
