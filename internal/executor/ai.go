package executor

import (
	"clampany/internal"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type AIExecutor struct {
	Role       internal.Role
	OutputDir  string
	SaveOutput bool
	PaneID     string // 割り当てられたtmuxペインID
}

func (e *AIExecutor) Execute(prompt string) error {
	fmt.Printf("[DEBUG] AIExecutor: %s へ送信: %s\n", e.PaneID, prompt)
	err := exec.Command("tmux", "send-keys", "-t", e.PaneID, prompt, "C-m").Run()
	exec.Command("tmux", "send-keys", "-t", e.PaneID, "Enter").Run()

	// --- 追加: AIの出力をinstructions.mdに追記 ---
	// 1秒待ってからペインの最新10行を取得
	exec.Command("sleep", "1").Run()
	out, errCap := exec.Command("tmux", "capture-pane", "-t", e.PaneID, "-p", "-S", "-10").Output()
	if errCap == nil {
		lines := strings.Split(string(out), "\n")
		output := strings.Join(lines, "\n")
		if strings.TrimSpace(output) != "" {
			roleSection := fmt.Sprintf("\n[%s]\n%s\n", e.PaneID, output)
			f, err := os.OpenFile("instructions.md", os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				f.WriteString(roleSection)
				f.Close()
				fmt.Printf("[DEBUG] instructions.mdに追記: %s\n", roleSection)
			}
		}
	}
	// --- ここまで追加 ---

	return err
}
