package repoconfig

import "github.com/atomicobject/rhizome/pkg/vault/config"

// TasksUserConfig is one person's scheduled-task settings on this machine
// ([[scheduled-tasks|SPEC-0127]] US10). None of it belongs in a task file.
type TasksUserConfig struct {
	// Timezone is the person's default schedule zone (an IANA name).
	Timezone string             `json:"timezone,omitempty" yaml:"timezone,omitempty"`
	Notify   *TasksNotifyConfig `json:"notify,omitempty" yaml:"notify,omitempty"`
	// Repositories is keyed by the canonical Git common directory.
	Repositories map[string]TasksRepositoryConfig `json:"repositories,omitempty" yaml:"repositories,omitempty"`
}

// TasksNotifyConfig runs a command after each run that notifies (US14).
type TasksNotifyConfig struct {
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
}

// TasksRepositoryConfig is how one repository's tasks run on this machine.
type TasksRepositoryConfig struct {
	// RunHere turns scheduled runs on for this machine; off by default.
	RunHere bool `json:"runHere,omitempty" yaml:"runHere,omitempty"`
	// Checkout is the checkout registered for task running; empty means the
	// main checkout.
	Checkout      string `json:"checkout,omitempty" yaml:"checkout,omitempty"`
	Push          bool   `json:"push,omitempty" yaml:"push,omitempty"`
	DailyTokenCap int64  `json:"dailyTokenCap,omitempty" yaml:"dailyTokenCap,omitempty"`
	// Tasks overrides the chat agent settings per task id.
	Tasks map[string]TaskAgentConfig `json:"tasks,omitempty" yaml:"tasks,omitempty"`
}

// TaskAgentConfig picks the harness, model, and effort for one task.
type TaskAgentConfig struct {
	Harness string `json:"harness,omitempty" yaml:"harness,omitempty"`
	Model   string `json:"model,omitempty" yaml:"model,omitempty"`
	Effort  string `json:"effort,omitempty" yaml:"effort,omitempty"`
}

// LoadTasksUserConfig reads the person's task settings; a missing file or
// section is empty settings.
func LoadTasksUserConfig(env config.Env) (TasksUserConfig, error) {
	cfg, err := LoadCliConfig(env, true)
	if err != nil || cfg.Tasks == nil {
		return TasksUserConfig{}, err
	}
	return *cfg.Tasks, nil
}

// UpdateTasksRepository edits one repository's task settings under the
// configuration file lock. An entry left empty is removed.
func UpdateTasksRepository(env config.Env, commonDir string, edit func(*TasksRepositoryConfig) error) error {
	return UpdateCliConfig(env, func(cfg *CliConfig) error {
		if cfg.Tasks == nil {
			cfg.Tasks = &TasksUserConfig{}
		}
		entry := cfg.Tasks.Repositories[commonDir]
		if err := edit(&entry); err != nil {
			return err
		}
		if cfg.Tasks.Repositories == nil {
			cfg.Tasks.Repositories = map[string]TasksRepositoryConfig{}
		}
		if isZeroRepository(entry) {
			delete(cfg.Tasks.Repositories, commonDir)
		} else {
			cfg.Tasks.Repositories[commonDir] = entry
		}
		if cfg.Tasks.Timezone == "" && cfg.Tasks.Notify == nil && len(cfg.Tasks.Repositories) == 0 {
			cfg.Tasks = nil
		}
		return nil
	})
}

func isZeroRepository(e TasksRepositoryConfig) bool {
	return !e.RunHere && e.Checkout == "" && !e.Push && e.DailyTokenCap == 0 && len(e.Tasks) == 0
}
