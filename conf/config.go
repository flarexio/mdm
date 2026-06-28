// Package conf loads mdm-server's YAML configuration.
package conf

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path     string   `yaml:"-"`
	Push     Push     `yaml:"push"`
	Identity Identity `yaml:"identity"`
	Enroll   Enroll   `yaml:"enroll"`
	Auth     Auth     `yaml:"auth"`
}

// Auth configures bearer-token verification for the admin endpoints against
// identity's JWKS. Leave jwksURL empty to leave those endpoints unauthenticated.
type Auth struct {
	JWKSURL  string `yaml:"jwksURL"`
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`
}

type Push struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type Identity struct {
	URL  string `yaml:"url"`
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type Enroll struct {
	Identifier   string `yaml:"identifier"`
	Organization string `yaml:"organization"`
	ExternalURL  string `yaml:"externalURL"`
	SCEP         SCEP   `yaml:"scep"`
}

type SCEP struct {
	URL    string `yaml:"url"`
	CAName string `yaml:"caName"`
}

// LoadConfig reads <path>/config.yaml, falling back to config.example.yaml.
func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path + "/config.yaml")
	if err != nil {
		f, err = os.Open(path + "/config.example.yaml")
		if err != nil {
			return nil, err
		}
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}

	cfg.Path = path
	return &cfg, nil
}
