package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type AppConfig struct {
	AccessKeyID    string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
}

type StorachaConfig struct {
	PrivateKey string `json:"privateKey"`
	ProofPath  string `json:"proofPath"`
	SpaceDID   string `json:"spaceDid"`
}

var (
	configDirOnce sync.Once
	configDir     string
	configDirErr  error
)

func ensureConfigDir() (string, error) {
	configDirOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			configDirErr = err
			return
		}
		configDir = filepath.Join(home, ".storacha-rclone")
		configDirErr = os.MkdirAll(configDir, 0o700)
	})
	return configDir, configDirErr
}

func ConfigPath() (string, error) {
	dir, err := ensureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func StorachaConfigPath() (string, error) {
	dir, err := ensureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "storacha.json"), nil
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (cfg AppConfig) Save() error {
	p, err := ConfigPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(p, b)
}

func Load() (AppConfig, error) {
	var cfg AppConfig
	p, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w (run `storacha-rclone aws-login` first)", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.Region == "" || cfg.Bucket == "" {
		return cfg, fmt.Errorf("config incomplete, run `storacha-rclone aws-login` again")
	}
	return cfg, nil
}

func (cfg StorachaConfig) Save() error {
	p, err := StorachaConfigPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(p, b)
}

func LoadStoracha() (StorachaConfig, error) {
	var cfg StorachaConfig
	p, err := StorachaConfigPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cfg, fmt.Errorf("read storacha config: %w (run `storacha-rclone storacha-login` first)", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.PrivateKey == "" || cfg.ProofPath == "" || cfg.SpaceDID == "" {
		return cfg, fmt.Errorf("storacha config incomplete, run `storacha-rclone storacha-login` again")
	}
	return cfg, nil
}
