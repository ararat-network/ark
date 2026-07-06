package chainstate

import (
	"errors"
	"strings"
	"time"
)

type Config struct {
	Address  string        `json:"address"`
	Timeout  time.Duration `json:"timeout"`
	Interval time.Duration `json:"interval"`
}

func (c Config) Equal(b Config) bool {
	return c.Address == b.Address && c.Timeout == b.Timeout && c.Interval == b.Interval
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return errors.New("vote targets query address cannot be empty")
	}
	if c.Timeout <= 0 {
		return errors.New("vote targets query timeout must be greater than 0")
	}
	if c.Interval <= 0 {
		return errors.New("vote targets poll interval must be greater than 0")
	}

	return nil
}
