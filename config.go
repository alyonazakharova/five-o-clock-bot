package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Token             string
	ChatID            int64 // 0 = setup mode: only /chatid works, no scheduled picks
	ExcludedUsernames map[string]bool
	ExcludedIDs       map[int64]bool
	Hour, Minute      int
	Location          *time.Location
	StatePath         string
	HTTPAddr          string // e.g. ":8080"; empty disables the HTTP API
	APIToken          string // optional bearer token for the HTTP API
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Token:             os.Getenv("BOT_TOKEN"),
		ExcludedUsernames: map[string]bool{},
		ExcludedIDs:       map[int64]bool{},
		Location:          time.Local,
		StatePath:         envOr("STATE_PATH", "./data/state.json"),
		HTTPAddr:          os.Getenv("HTTP_ADDR"),
		APIToken:          os.Getenv("API_TOKEN"),
	}
	if cfg.Token == "" {
		return nil, errors.New("BOT_TOKEN is required")
	}

	if v := os.Getenv("CHAT_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CHAT_ID: %w", err)
		}
		cfg.ChatID = id
	}

	for _, item := range strings.Split(os.Getenv("EXCLUDE"), ",") {
		item = strings.TrimPrefix(strings.TrimSpace(item), "@")
		if item == "" {
			continue
		}
		// Telegram usernames can't be purely numeric, so a number is a user id.
		if id, err := strconv.ParseInt(item, 10, 64); err == nil {
			cfg.ExcludedIDs[id] = true
		} else {
			cfg.ExcludedUsernames[strings.ToLower(item)] = true
		}
	}

	t, err := time.Parse("15:04", envOr("DUTY_TIME", "17:00"))
	if err != nil {
		return nil, fmt.Errorf("DUTY_TIME must be HH:MM: %w", err)
	}
	cfg.Hour, cfg.Minute = t.Hour(), t.Minute()

	if tz := os.Getenv("TZ"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("TZ: %w", err)
		}
		cfg.Location = loc
	}

	return cfg, nil
}

func (c *Config) IsExcluded(m *Member) bool {
	return c.ExcludedIDs[m.ID] || (m.Username != "" && c.ExcludedUsernames[strings.ToLower(m.Username)])
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
