// Package config loads the service configuration from a YAML file and
// environment variables. Environment variables win over the file so that a
// single binary can be re-targeted without editing files.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Retention struct {
	RawDays      int `yaml:"raw_days"`
	Rollup5mDays int `yaml:"rollup_5m_days"`
	Rollup1hDays int `yaml:"rollup_1h_days"`
}

type Probe struct {
	// MaxConcurrent caps how many rounds may run at the same time.
	MaxConcurrent int `yaml:"max_concurrent"`
	// WriteBatchSeconds is how long results are buffered before one
	// transaction commits them.
	WriteBatchSeconds int `yaml:"write_batch_seconds"`
	// DNSCacheMinutes caches name resolution so it is not re-done every round.
	DNSCacheMinutes int `yaml:"dns_cache_minutes"`
	// PacketGapMs is the delay between packets inside one round.
	PacketGapMs int `yaml:"packet_gap_ms"`
}

type Config struct {
	Listen            string `yaml:"listen"`
	DataDir           string `yaml:"data_dir"`
	AdminPasswordHash string `yaml:"admin_password_hash"`
	SessionTTLHours   int    `yaml:"session_ttl_hours"`
	// TrustProxy enables X-Forwarded-For parsing for the login rate limiter.
	TrustProxy bool      `yaml:"trust_proxy"`
	Retention  Retention `yaml:"retention"`
	Probe      Probe     `yaml:"probe"`

	// Path is the file this configuration was loaded from (may be empty).
	Path string `yaml:"-"`
}

func Defaults() Config {
	return Config{
		Listen:          "0.0.0.0:8090",
		DataDir:         "/var/lib/linkprobe",
		SessionTTLHours: 72,
		Retention: Retention{
			RawDays:      7,
			Rollup5mDays: 90,
			Rollup1hDays: 730,
		},
		Probe: Probe{
			MaxConcurrent:     32,
			WriteBatchSeconds: 30,
			DNSCacheMinutes:   5,
			PacketGapMs:       400,
		},
	}
}

// Load reads cfgPath (optional) on top of the defaults, then applies env vars.
func Load(cfgPath string) (Config, error) {
	cfg := Defaults()
	if cfgPath != "" {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return cfg, fmt.Errorf("read config: %w", err)
			}
		} else {
			if err := yaml.Unmarshal(b, &cfg); err != nil {
				return cfg, fmt.Errorf("parse config: %w", err)
			}
			cfg.Path = cfgPath
		}
	}
	applyEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyEnv(c *Config) {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
		}
	}
	num := func(key string, dst *int) {
		if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				*dst = n
			}
		}
	}
	boolean := func(key string, dst *bool) {
		if v, ok := os.LookupEnv(key); ok {
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				*dst = b
			}
		}
	}
	str("LINKPROBE_LISTEN", &c.Listen)
	str("LINKPROBE_DATA_DIR", &c.DataDir)
	str("LINKPROBE_ADMIN_PASSWORD_HASH", &c.AdminPasswordHash)
	num("LINKPROBE_SESSION_TTL_HOURS", &c.SessionTTLHours)
	boolean("LINKPROBE_TRUST_PROXY", &c.TrustProxy)
	num("LINKPROBE_RAW_RETENTION_DAYS", &c.Retention.RawDays)
	num("LINKPROBE_ROLLUP_5M_RETENTION_DAYS", &c.Retention.Rollup5mDays)
	num("LINKPROBE_ROLLUP_1H_RETENTION_DAYS", &c.Retention.Rollup1hDays)
	num("LINKPROBE_MAX_CONCURRENT", &c.Probe.MaxConcurrent)
	num("LINKPROBE_WRITE_BATCH_SECONDS", &c.Probe.WriteBatchSeconds)
	num("LINKPROBE_DNS_CACHE_MINUTES", &c.Probe.DNSCacheMinutes)
	num("LINKPROBE_PACKET_GAP_MS", &c.Probe.PacketGapMs)
}

func (c Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if strings.TrimSpace(c.AdminPasswordHash) == "" {
		return fmt.Errorf("admin_password_hash is required (generate one with: linkprobe hash-password)")
	}
	if !strings.HasPrefix(c.AdminPasswordHash, "$2") {
		return fmt.Errorf("admin_password_hash must be a bcrypt hash")
	}
	if c.SessionTTLHours < 1 || c.SessionTTLHours > 24*30 {
		return fmt.Errorf("session_ttl_hours out of range")
	}
	if c.Retention.RawDays < 1 || c.Retention.RawDays > 365 {
		return fmt.Errorf("retention.raw_days out of range")
	}
	if c.Retention.Rollup5mDays < c.Retention.RawDays || c.Retention.Rollup5mDays > 3650 {
		return fmt.Errorf("retention.rollup_5m_days must be >= raw_days and <= 3650")
	}
	if c.Retention.Rollup1hDays < c.Retention.Rollup5mDays || c.Retention.Rollup1hDays > 3650 {
		return fmt.Errorf("retention.rollup_1h_days must be >= rollup_5m_days and <= 3650")
	}
	if c.Probe.MaxConcurrent < 1 || c.Probe.MaxConcurrent > 512 {
		return fmt.Errorf("probe.max_concurrent out of range")
	}
	if c.Probe.WriteBatchSeconds < 1 || c.Probe.WriteBatchSeconds > 300 {
		return fmt.Errorf("probe.write_batch_seconds out of range")
	}
	if c.Probe.PacketGapMs < 50 || c.Probe.PacketGapMs > 2000 {
		return fmt.Errorf("probe.packet_gap_ms out of range")
	}
	if c.Probe.DNSCacheMinutes < 1 || c.Probe.DNSCacheMinutes > 1440 {
		return fmt.Errorf("probe.dns_cache_minutes out of range")
	}
	return nil
}
