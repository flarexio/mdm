// Package conf loads mdm-server's YAML configuration.
package conf

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/flarexio/core/pubsub"
)

type Config struct {
	Path     string   `yaml:"-"`
	Name     string   `yaml:"name"` // instance name: NATS connection + per-instance durable consumer
	CA       string   `yaml:"ca"`   // FlareX root: verifies identity's server cert + device client certs, and is the enrollment trust anchor
	Push     Push     `yaml:"push"`
	Identity Identity `yaml:"identity"`
	Enroll   Enroll   `yaml:"enroll"`
	Auth     Auth     `yaml:"auth"`
	Redis    Redis    `yaml:"redis"`
	EventBus EventBus `yaml:"eventBus"`
}

// Redis configures the shared strong-consistency enrollment cache.
type Redis struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// EventBus configures the NATS JetStream streams and consumers.
type EventBus struct {
	Enrollments pubsub.StreamConsumer `yaml:"enrollments"`
	Commands    pubsub.StreamConsumer `yaml:"commands"`
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
	if err := yaml.NewDecoder(NewEnvExpandedReader(f)).Decode(&cfg); err != nil {
		return nil, err
	}

	cfg.Path = path
	return &cfg, nil
}
