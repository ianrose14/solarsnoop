package internal

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type SecretsFile struct {
	Ecobee struct {
		ApiKey string `yaml:"apikey"`
	}
	Enphase struct {
		ApiKey       string `yaml:"apikey"`
		ClientID     string `yaml:"clientId"`
		ClientSecret string `yaml:"clientSecret"`
	}
	SendGrid struct {
		ApiKey string `yaml:"apikey"`
	} `yaml:"sendgrid"`
}

func ParseSecrets(filename string) (*SecretsFile, error) {
	fp, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer fp.Close()

	var dst SecretsFile
	if err := yaml.NewDecoder(fp).Decode(&dst); err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	return &dst, nil
}
