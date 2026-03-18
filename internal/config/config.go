package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TTS    TTSConfig    `yaml:"tts"`
	STT    STTConfig    `yaml:"stt"`
	Review ReviewConfig `yaml:"review"`
	MCP    MCPConfig    `yaml:"mcp"`
}

type TTSConfig struct {
	Adapter  string  `yaml:"adapter"`
	Voice    string  `yaml:"voice"`
	Rate     int     `yaml:"rate"`
	APIKey   string  `yaml:"api_key"`
	Model    string  `yaml:"model"`
	Speed    float64 `yaml:"speed"`
}

type STTConfig struct {
	Adapter        string `yaml:"adapter"`
	Model          string `yaml:"model"`
	SilenceTimeout string `yaml:"silence_timeout"`
	APIKey         string `yaml:"api_key"`
}

type ReviewConfig struct {
	StateFile string `yaml:"state_file"`
	AutoSave  bool   `yaml:"auto_save"`
}

type MCPConfig struct {
	SocketPath string `yaml:"socket_path"`
}

func Default() *Config {
	return &Config{
		TTS: TTSConfig{
			Adapter: "say",
			Voice:   "Daniel",
			Rate:    195,
		},
		STT: STTConfig{
			Adapter:        "whisper",
			Model:          "medium.en",
			SilenceTimeout: "3.0",
		},
		Review: ReviewConfig{
			StateFile: ".yap-state.json",
			AutoSave:  true,
		},
	}
}

type Persona struct {
	VoiceStyle  string   `yaml:"voice_style"`
	ReviewFocus string   `yaml:"review_focus"`
	Ignore      string   `yaml:"ignore"`
	Vocabulary  []string `yaml:"vocabulary"`
}

func LoadPersona(path string) (*Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // optional file
		}
		return nil, fmt.Errorf("read persona: %w", err)
	}
	var p Persona
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &p, nil
}

func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
