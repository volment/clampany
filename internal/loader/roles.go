package loader

import (
	"os"
	"gopkg.in/yaml.v3"
	"clampany/internal"
)

type RolesFile struct {
	Roles []internal.Role `yaml:"roles"`
}

func LoadRoles(path string) ([]internal.Role, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rf RolesFile
	if err := yaml.NewDecoder(f).Decode(&rf); err != nil {
		return nil, err
	}
	return rf.Roles, nil
} 