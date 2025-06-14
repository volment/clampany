package executor

import (
	"bufio"
	"clampany/internal"
	"fmt"
	"os"
	"os/exec"
)

type HumanExecutor struct{}

func (e *HumanExecutor) Execute(t internal.Task, in string) (string, error) {
	_, tmuxOk := os.LookupEnv("TMUX")
	if tmuxOk {
		args := []string{"split-window", "-v", "-c", os.Getenv("PWD")}
		outputPath := os.Getenv("CLAMPANY_OUTPUT_PATH")
		if outputPath == "" {
			outputPath = "outputs/" + t.Name + ".md"
		}
		cmdStr := "mkdir -p outputs; echo '" + t.Prompt + "' | tee '" + outputPath + "'; read -p '回答を入力しEnterで閉じてください: ' ans; echo $ans | tee -a '" + outputPath + "'"
		args = append(args, cmdStr)
		tmuxCmd := exec.Command("tmux", args...)
		_, err := tmuxCmd.CombinedOutput()
		if err != nil {
			return "[tmuxペインでhuman実行エラー]", err
		}
		return "[tmuxペインでhuman実行] (出力はoutputs/" + t.Name + ".md) に保存されました)", nil
	}
	fmt.Println("[HUMAN TASK]", t.Prompt)
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	scanner.Scan()
	return scanner.Text(), nil
}
