package internal

type RoleType string

const (
	RoleAI    RoleType = "ai"
	RoleShell RoleType = "shell"
	RoleHuman RoleType = "human"
)

type Role struct {
	Name     string   `yaml:"name"`
	Type     RoleType `yaml:"type"`
	Model    string   `yaml:"model,omitempty"`
	Behavior string   `yaml:"behavior,omitempty"`
	APIKey   string   `yaml:"api_key,omitempty"`
}

type Task struct {
	Name      string   `yaml:"name"`
	Role      string   `yaml:"role"`
	Prompt    string   `yaml:"prompt"`
	Command   string   `yaml:"command,omitempty"`
	DependsOn []string `yaml:"depends_on"`
}

type Executor interface {
	Execute(t Task, in string) (out string, err error)
}
