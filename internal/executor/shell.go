package executor

import (
	"clampany/internal"
	"os"
	"os/exec"
)

type ShellExecutor struct{}

func (e *ShellExecutor) Execute(t internal.Task, in string) (string, error) {
	_, tmuxOk := os.LookupEnv("TMUX")
	if tmuxOk {
		args := []string{"split-window", "-v", "-c", os.Getenv("PWD")}
		outputPath := os.Getenv("CLAMPANY_OUTPUT_PATH")
		if outputPath == "" {
			outputPath = "outputs/" + t.Name + ".md"
		}
		cmdStr := "mkdir -p outputs; " + t.Command + " | tee '" + outputPath + "' ; read -p 'Press Enter to close...'"
		args = append(args, cmdStr)
		tmuxCmd := exec.Command("tmux", args...)
		_, err := tmuxCmd.CombinedOutput()
		if err != nil {
			return "[tmuxペインでshell実行エラー]", err
		}
		return "[tmuxペインでshell実行] (出力はoutputs/" + t.Name + ".md) に保存されました)", nil
	}
	cmd := exec.Command("bash", "-c", t.Command)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
