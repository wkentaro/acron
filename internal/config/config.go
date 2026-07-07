package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/robfig/cron/v3"
	"github.com/wkentaro/acron/internal/paths"
)

type Job struct {
	Name      string            `toml:"name"`
	Schedule  string            `toml:"schedule"`
	Agent     []string          `toml:"agent"`
	Prompt    string            `toml:"prompt"`
	Cwd       string            `toml:"cwd"`
	Enabled   *bool             `toml:"enabled"`
	Timeout   string            `toml:"timeout"`
	Env       map[string]string `toml:"env"`
	Condition []string          `toml:"condition"`
}

type Config struct {
	Jobs []Job `toml:"job"`
}

func (j Job) IsEnabled() bool {
	return j.Enabled == nil || *j.Enabled
}

func (c *Config) Job(name string) (Job, error) {
	for _, job := range c.Jobs {
		if job.Name == name {
			return job, nil
		}
	}
	return Job{}, fmt.Errorf("no job named %q", name)
}

const DefaultTimeout = time.Hour

func (j Job) ResolvedTimeout() (time.Duration, error) {
	if j.Timeout == "" {
		return DefaultTimeout, nil
	}
	return time.ParseDuration(j.Timeout)
}

func (j Job) NextFire(after time.Time) (time.Time, error) {
	schedule, err := scheduleParser.Parse(j.Schedule)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(after), nil
}

func DefaultPath() string {
	if env := os.Getenv("ACRON_CONFIG"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "acron", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "acron", "config.toml")
}

func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return &cfg, nil
}

var namePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

var scheduleParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
)

func (c *Config) Validate() error {
	var problems []string
	seen := make(map[string]bool, len(c.Jobs))
	for i, job := range c.Jobs {
		problems = append(problems, validateJob(job, i, seen)...)
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("invalid config:\n  " + strings.Join(problems, "\n  "))
}

func validateJob(job Job, index int, seen map[string]bool) []string {
	label := fmt.Sprintf("job[%d]", index)
	if job.Name != "" {
		label = fmt.Sprintf("job %q", job.Name)
	}

	var problems []string
	report := func(msg string) { problems = append(problems, label+": "+msg) }

	switch {
	case job.Name == "":
		report("name is required")
	case !namePattern.MatchString(job.Name):
		report("name must match [a-z0-9_-]")
	case seen[job.Name]:
		report("duplicate name")
	default:
		seen[job.Name] = true
	}

	if job.Schedule == "" {
		report("schedule is required")
	} else if _, err := scheduleParser.Parse(job.Schedule); err != nil {
		report(fmt.Sprintf("unparseable schedule %q: %v", job.Schedule, err))
	}

	if len(job.Agent) == 0 {
		report("agent is required")
	} else if job.Agent[0] == "" {
		report("agent command is empty")
	}

	if job.Prompt == "" {
		report("prompt is required")
	}

	if job.Cwd == "" {
		report("cwd is required")
	} else if dir := paths.ExpandHome(job.Cwd); !isDir(dir) {
		report(fmt.Sprintf("cwd does not exist: %s", dir))
	}

	if _, err := job.ResolvedTimeout(); err != nil {
		report(fmt.Sprintf("invalid timeout %q: %v", job.Timeout, err))
	}

	if len(job.Condition) > 0 && job.Condition[0] == "" {
		report("condition command is empty")
	}

	return problems
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
