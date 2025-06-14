package cmd

import (
	"clampany/internal/executor"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "clampany",
	Short: "Clampany CLI root command",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			// 永続型ワーカーモード起動
			startPersistentWorkers()
		}
	},
}

//go:embed instructions/*.md
var instructionsFS embed.FS

func extractInstructions() error {
	entries, _ := fs.ReadDir(instructionsFS, "instructions")
	os.MkdirAll("instructions", 0755)
	for _, entry := range entries {
		data, _ := instructionsFS.ReadFile("instructions/" + entry.Name())
		fpath := filepath.Join("instructions", entry.Name())
		if _, err := os.Stat(fpath); os.IsNotExist(err) {
			os.WriteFile(fpath, data, 0644)
		}
	}
	return nil
}

func startPersistentWorkers() {
	extractInstructions()
	// roles.yamlがなくてもエラーにしない。<role>_queue.mdやinstructions/<role>.mdからロール一覧を自動検出
	aiRoles := []string{}
	entries, err := os.ReadDir("instructions")
	if err == nil {
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "sufix.md" {
				role := strings.TrimSuffix(entry.Name(), ".md")
				aiRoles = append(aiRoles, role)
			}
		}
	}
	// fallback: *_queue.mdからもロール名を検出
	queueEntries, err := os.ReadDir(".")
	for _, entry := range queueEntries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), "_queue.md") {
			role := strings.TrimSuffix(entry.Name(), "_queue.md")
			found := false
			for _, r := range aiRoles {
				if r == role {
					found = true
					break
				}
			}
			if !found {
				aiRoles = append(aiRoles, role)
			}
		}
	}
	if len(aiRoles) == 0 {
		fmt.Println("AIロールが見つかりません（instructions/*.mdや*_queue.mdを確認してください）")
		os.Exit(1)
	}

	// ceoを最後に回す
	ceoIdx := -1
	for i, r := range aiRoles {
		if r == "ceo" || strings.HasPrefix(r, "ceo") {
			ceoIdx = i
			break
		}
	}
	if ceoIdx != -1 && ceoIdx != len(aiRoles)-1 {
		ceoRole := aiRoles[ceoIdx]
		aiRoles = append(aiRoles[:ceoIdx], aiRoles[ceoIdx+1:]...)
		aiRoles = append(aiRoles, ceoRole)
	}

	// 2. ロールごとにペイン生成＋claude起動＋ラベル付与
	paneMap := map[string]string{}
	for _, role := range aiRoles {
		cmd := exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "bash")
		out, err := cmd.Output()
		if err != nil {
			fmt.Printf("tmuxペイン作成失敗(%s): %v\n", role, err)
			os.Exit(1)
		}
		paneID := strings.TrimSpace(string(out))
		paneMap[role] = paneID
		if err := exec.Command("tmux", "select-pane", "-t", paneID, "-T", role).Run(); err != nil {
			fmt.Printf("ラベル付与失敗(%s): %v\n", role, err)
		}
		// まずclaudeを起動
		if err := exec.Command("tmux", "send-keys", "-t", paneID, "claude --dangerously-skip-permissions", "C-m").Run(); err != nil {
			fmt.Printf("claude起動失敗(%s): %v\n", role, err)
		}
		time.Sleep(800 * time.Millisecond)
		// instructions/<rolebase>.mdとsufix.mdの内容を1行ずつ送信
		roleBase := role
		if strings.HasSuffix(role, "1") || strings.HasSuffix(role, "2") || strings.HasSuffix(role, "3") || strings.HasSuffix(role, "4") || strings.HasSuffix(role, "5") {
			roleBase = strings.TrimRight(role, "0123456789")
		}
		roleContent, _ := os.ReadFile(fmt.Sprintf("instructions/%s.md", roleBase))
		sufixContent, _ := os.ReadFile("instructions/sufix.md")
		for _, line := range strings.Split(string(roleContent), "\n") {
			if strings.TrimSpace(line) != "" {
				exec.Command("tmux", "send-keys", "-t", paneID, line, "C-m").Run()
				exec.Command("tmux", "send-keys", "-t", paneID, "Enter").Run()
				time.Sleep(80 * time.Millisecond)
			}
		}
		for _, line := range strings.Split(string(sufixContent), "\n") {
			if strings.TrimSpace(line) != "" {
				exec.Command("tmux", "send-keys", "-t", paneID, line, "C-m").Run()
				exec.Command("tmux", "send-keys", "-t", paneID, "Enter").Run()
				time.Sleep(80 * time.Millisecond)
			}
		}
		fmt.Printf("[DEBUG] ペイン生成: %s → %s\n", role, paneID)
		time.Sleep(1 * time.Second)
	}
	exec.Command("tmux", "select-layout", "tiled").Run()

	// 3. ロールごとにキュー(chan string)生成
	queues := map[string]chan string{}
	for _, role := range aiRoles {
		queues[role] = make(chan string, 100)
	}

	// 4. ロールごとに永続ワーカー起動
	for _, role := range aiRoles {
		go func(role string) {
			execAI := &executor.AIExecutor{PaneID: paneMap[role]}
			for prompt := range queues[role] {
				execAI.Execute(prompt)
			}
		}(role)
	}

	// 5. panes.json保存
	os.MkdirAll("run/latest", 0755)
	f, _ := os.Create("run/latest/panes.json")
	json.NewEncoder(f).Encode(paneMap)
	f.Close()

	fmt.Println("[Clampany] 全ロール永続ワーカー起動中。Ctrl+Cで終了")

	// 6. 各ロールごとに<role>_queue.mdを監視し、指示を自分のキューに流し込む
	for _, role := range aiRoles {
		roleBase := role
		if strings.HasSuffix(role, "1") || strings.HasSuffix(role, "2") || strings.HasSuffix(role, "3") || strings.HasSuffix(role, "4") || strings.HasSuffix(role, "5") {
			roleBase = strings.TrimRight(role, "0123456789")
		}
		queueFile := fmt.Sprintf("clampany_queue/%s_queue.md", roleBase)
		go func(role, queueFile string) {
			lastContent := ""
			for {
				b, err := os.ReadFile(queueFile)
				if err == nil {
					content := string(b)
					if content != lastContent {
						start := "[" + role + "]"
						if idx := strings.Index(content, start); idx != -1 {
							msg := extractSection(content, start)
							if msg != "" {
								queues[role] <- msg
							}
						}
						lastContent = content
					}
				}
				time.Sleep(2 * time.Second)
			}
		}(role, queueFile)
	}

	// 7. Ctrl+Cまで無限待機
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("[Clampany] 終了します")

	// 8. 各ペインの出力を監視し、clampany inqueue ...が出たら即時実行（複数行対応・永続履歴）
	for _, role := range aiRoles {
		paneID := paneMap[role]
		go func(paneID string) {
			const logFile = "executed_inqueue.log"
			maxLogLines := 1000
			loadExecuted := func() map[string]bool {
				executed := map[string]bool{}
				b, err := os.ReadFile(logFile)
				if err == nil {
					lines := strings.Split(string(b), "\n")
					for _, l := range lines {
						if l != "" {
							executed[l] = true
						}
					}
				}
				return executed
			}
			appendHash := func(hash string) {
				b, _ := os.ReadFile(logFile)
				lines := strings.Split(string(b), "\n")
				lines = append(lines, hash)
				if len(lines) > maxLogLines {
					lines = lines[len(lines)-maxLogLines:]
				}
				os.WriteFile(logFile, []byte(strings.Join(lines, "\n")), 0644)
			}
			for {
				out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-S", "-50").Output()
				if err != nil {
					fmt.Fprintf(os.Stderr, "❌ tmux capture error on %s: %v\n", paneID, err)
					time.Sleep(2 * time.Second)
					continue
				}
				lines := string(out)
				re := regexp.MustCompile(`(?s)clampany inqueue [^\n]+?"[^"]*"`)
				matches := re.FindAllString(lines, -1)
				executed := loadExecuted()
				for _, cmd := range matches {
					cmd1line := strings.ReplaceAll(cmd, "\n", " ")
					cmd1line = strings.ReplaceAll(cmd1line, "  ", " ")
					hash := fmt.Sprintf("%x", sha256.Sum256([]byte(cmd1line)))
					if !executed[hash] {
						fmt.Println("🟢 実行:", cmd1line)
						go func(c, h string) {
							if err := exec.Command("sh", "-c", c).Run(); err != nil {
								fmt.Fprintf(os.Stderr, "❌ 実行失敗: %s: %v\n", c, err)
							}
							appendHash(h)
						}(cmd1line, hash)
					}
				}
				time.Sleep(2 * time.Second)
			}
		}(paneID)
	}
}

// 指定ロールの指示セクションを抽出
func extractSection(content, start string) string {
	idx := strings.Index(content, start)
	if idx == -1 {
		return ""
	}
	remain := content[idx+len(start):]
	end := strings.Index(remain, "[")
	if end == -1 {
		return strings.TrimSpace(remain)
	}
	return strings.TrimSpace(remain[:end])
}

func Execute() error {
	return rootCmd.Execute()
}

// build時にinstructionsディレクトリがなければ作成
func init() {
	os.MkdirAll("instructions", 0755)
	os.MkdirAll("clampany_queue", 0755)
}
