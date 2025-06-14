package loader

import (
	"os"
	"gopkg.in/yaml.v3"
	"clampany/internal"
)

type TasksFile struct {
	Tasks []internal.Task `yaml:"tasks"`
}

func LoadTasks(path string) ([]internal.Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tf TasksFile
	if err := yaml.NewDecoder(f).Decode(&tf); err != nil {
		return nil, err
	}
	return tf.Tasks, nil
} 