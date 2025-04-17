package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/asaskevich/govalidator"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"github.com/pkg/errors"
	th "github.com/teran/go-time"
	yaml "gopkg.in/yaml.v3"
)

var isCIDR = validation.NewStringRuleWithError(govalidator.IsCIDR, validation.NewError("validation_is_cidr", "must be a valid CIDR"))

var (
	_ validation.Validatable = (*Config)(nil)
	_ validation.Validatable = (*Announcer)(nil)
	_ validation.Validatable = (*Service)(nil)
	_ validation.Validatable = (*Metrics)(nil)
	_ validation.Validatable = (*Check)(nil)
	_ validation.Validatable = (*Peer)(nil)
)

type Announcer struct {
	RouterID     string   `json:"router_id"`
	LocalAddress string   `json:"local_address"`
	LocalASN     uint32   `json:"local_asn"`
	Routes       []string `json:"routes"`
	Peers        []Peer   `json:"peers"`
}

func (a Announcer) Validate() error {
	return validation.ValidateStruct(&a,
		validation.Field(&a.RouterID, validation.Required, is.IPv4),
		validation.Field(&a.LocalAddress, validation.Required, is.IPv4),
		validation.Field(&a.LocalASN, validation.Required),
		validation.Field(&a.Routes, validation.Required, validation.Each(isCIDR)),
		validation.Field(&a.Peers, validation.Required),
	)
}

type Check struct {
	Kind string          `json:"kind"`
	Spec json.RawMessage `json:"spec"`
}

func (c Check) Validate() error {
	return validation.ValidateStruct(&c,
		validation.Field(&c.Kind, validation.Required),
		validation.Field(&c.Spec, validation.Required),
	)
}

type Peer struct {
	Name              string      `json:"name"`
	RemoteAddress     string      `json:"remote_address"`
	RemoteASN         uint32      `json:"remote_asn"`
	HoldTime          th.Duration `json:"hold_time"`
	KeepaliveInterval th.Duration `json:"keepalive_interval"`
}

func (p Peer) Validate() error {
	return validation.ValidateStruct(&p,
		validation.Field(&p.Name, validation.Required),
		validation.Field(&p.RemoteAddress, validation.Required, is.IPv4),
		validation.Field(&p.RemoteASN, validation.Required),
	)
}

type Service struct {
	Name          string      `json:"name"`
	CheckInterval th.Duration `json:"check_interval"`
	Checks        []Check     `json:"checks"`
}

func (s Service) Validate() error {
	return validation.ValidateStruct(&s,
		validation.Field(&s.Name, validation.Required),
		validation.Field(&s.CheckInterval, validation.Required),
		validation.Field(&s.Checks, validation.Required),
	)
}

type Metrics struct {
	Enabled bool   `json:"enabled"`
	Address string `json:"address"`
}

func (m Metrics) Validate() error {
	return validation.ValidateStruct(&m,
		validation.Field(&m.Enabled, validation.Required),
		validation.Field(&m.Address, validation.Required),
	)
}

type Config struct {
	Announcer Announcer `json:"announcer"`
	Services  []Service `json:"services"`
	Metrics   Metrics   `json:"metrics"`
}

func (c *Config) Validate() error {
	return validation.ValidateStruct(c,
		validation.Field(&c.Announcer, validation.Required),
		validation.Field(&c.Services, validation.Required),
		validation.Field(&c.Metrics, validation.Required),
	)
}

func NewFromFile(filename string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrap(err, "error reading configuration file")
	}

	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".yml", ".yaml":
		type intermediate struct {
			Announcer map[string]any `yaml:"announcer"`
			Services  []any          `yaml:"services"`
			Metrics   map[string]any `yaml:"metrics"`
		}

		d := intermediate{}

		err := yaml.Unmarshal(data, &d)
		if err != nil {
			return nil, errors.Wrap(err, "error unmarshaling intermediate configuration")
		}

		data, err = json.Marshal(d)
		if err != nil {
			return nil, errors.Wrap(err, "error marshaling intermediate configuration")
		}
	case ".json":
		// no additional action needed
	default:
		return nil, errors.Errorf("unexpected file format: `%s`", ext)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errors.Wrap(err, "error unmarshaling config")
	}

	return cfg, cfg.Validate()
}
