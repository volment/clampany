package cmd

import (
	"clampany/internal/executor"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

//go:embed instructions/*.md
var instructionsFS embed.FS

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

var instructionFiles = []string{"ceo.md", "engineer.md", "planner.md", "pm.md", "sufix.md"}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "_clampany/instructionsディレクトリを作成し、バイナリに含まれているcmd/instructions/*.mdをコピーする",
	Run: func(cmd *cobra.Command, args []string) {
		os.MkdirAll("_clampany/instructions", 0755)
		// バイナリ内にどのファイルが埋め込まれているか確認
		entries, err := fs.ReadDir(instructionsFS, "instructions")
		if err != nil {
			fmt.Println("[ERROR] バイナリに埋め込まれているファイル一覧を取得できませんでした")
		} else {
			fmt.Println("[INFO] バイナリに埋め込まれているファイル:")
			for _, entry := range entries {
				fmt.Println(" -", entry.Name())
			}
		}
		for _, fname := range instructionFiles {
			b, err := instructionsFS.ReadFile("instructions/" + fname)
			if err != nil {
				fmt.Printf("%sの読み込み失敗: %v\n", fname, err)
				continue
			}
			os.WriteFile("_clampany/instructions/"+fname, b, 0644)
		}
		fmt.Println("_clampany/instructions ディレクトリを初期化しました")
	},
}

// 埋め込み→外部ファイルの順で読む関数
func readInstructionFile(name string) ([]byte, error) {
	return os.ReadFile("_clampany/instructions/" + name)
}

// ディレクトリ一覧も埋め込み→外部ファイル順で取得
func readInstructionDir() ([]fs.DirEntry, error) {
	return os.ReadDir("_clampany/instructions")
}

func startPersistentWorkers() {
	if _, err := os.Stat("_clampany/instructions"); os.IsNotExist(err) {
		os.MkdirAll("_clampany/instructions", 0755)
		entries, _ := os.ReadDir("cmd/instructions")
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") {
				src, _ := os.Open("cmd/instructions/" + entry.Name())
				dst, _ := os.Create("_clampany/instructions/" + entry.Name())
				io.Copy(dst, src)
				src.Close()
				dst.Close()
			}
		}
	}
	os.MkdirAll("_clampany/queue", 0755)
	// roles.yamlがなくてもエラーにしない。<role>_queue.mdやinstructions/<role>.mdからロール一覧を自動検出
	aiRoles := []string{}
	entries, err := readInstructionDir()
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
		fmt.Println("AIロールが見つかりません（cmd/instructions/*.mdや*_queue.mdを確認してください）")
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

	// ペインごとにreadyフラグを用意
	ready := map[string]bool{}
	// 2. ロールごとにペイン生成＋claude起動＋ラベル付与
	paneMap := map[string]string{}
	paneStatus := map[string]string{} // "init", "waiting", "running"
	paneStatusCount := map[string]int{}
	skippedFirstInqueue := map[string]bool{}
	// seenTokens := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(aiRoles))
	for _, role := range aiRoles {
		go func(role string) {
			defer wg.Done()
			cmd := exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "bash")
			out, err := cmd.Output()
			if err != nil {
				fmt.Printf("tmuxペイン作成失敗(%s): %v\n", role, err)
				os.Exit(1)
			}
			paneID := strings.TrimSpace(string(out))
			mu.Lock()
			paneMap[role] = paneID
			ready[role] = false // 初期状態は未起動
			paneStatus[role] = "init"
			paneStatusCount[role] = 0
			skippedFirstInqueue[role] = false
			mu.Unlock()
			if err := exec.Command("tmux", "select-pane", "-t", paneID, "-T", role).Run(); err != nil {
				fmt.Printf("ラベル付与失敗(%s): %v\n", role, err)
			}
			roleBase := role
			if strings.HasSuffix(role, "1") || strings.HasSuffix(role, "2") || strings.HasSuffix(role, "3") || strings.HasSuffix(role, "4") || strings.HasSuffix(role, "5") {
				roleBase = strings.TrimRight(role, "0123456789")
			}
			roleContent, _ := readInstructionFile(roleBase + ".md")
			sufixContent, _ := readInstructionFile("sufix.md")
			prompt := strings.TrimSpace(string(roleContent) + "\n" + string(sufixContent))
			prompt = strings.ReplaceAll(prompt, "'", "'\\''")
			tmpfile, _ := os.CreateTemp("", "clampany_prompt_*.txt")
			defer os.Remove(tmpfile.Name())
			tmpfile.WriteString(prompt)
			tmpfile.Close()
			cmdStr := fmt.Sprintf("claude --dangerously-skip-permissions \"$(cat %s)\"", tmpfile.Name())
			if err := exec.Command("tmux", "send-keys", "-t", paneID, cmdStr, "C-m").Run(); err != nil {
				fmt.Printf("claude起動失敗(%s): %v\n", role, err)
			}
			time.Sleep(800 * time.Millisecond)
			roleBase = role
			if strings.HasSuffix(role, "1") || strings.HasSuffix(role, "2") || strings.HasSuffix(role, "3") || strings.HasSuffix(role, "4") || strings.HasSuffix(role, "5") {
				roleBase = strings.TrimRight(role, "0123456789")
			}
			roleContent, _ = readInstructionFile(roleBase + ".md")
			sufixContent, _ = readInstructionFile("sufix.md")
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
			go func(r string) {
				time.Sleep(2000 * time.Millisecond)
				mu.Lock()
				ready[r] = true
				paneStatus[r] = "waiting"
				mu.Unlock()
			}(role)
			time.Sleep(1 * time.Second)
		}(role)
	}
	wg.Wait()
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
		queueFile := fmt.Sprintf("clampany/queue/%s_queue.md", roleBase)
		go func(role, queueFile string) {
			for {
				files, _ := filepath.Glob(fmt.Sprintf("_clampany/queue/%s_queue_*.md", roleBase))
				for _, queueFile := range files {
					b, err := os.ReadFile(queueFile)
					if err == nil {
						msg := strings.TrimSpace(string(b))
						if msg != "" {
							queues[role] <- msg
							os.Remove(queueFile) // キュー投入後にファイルごと削除
						}
					}
				}
				time.Sleep(2 * time.Second)
			}
		}(role, queueFile)
	}

	// 8. 各ペインの出力を監視し、clampany inqueue ...が出たら即時実行（複数行対応・永続履歴）
	/*
		for _, role := range aiRoles {
			paneID := paneMap[role]
			go func(paneID, role string) {
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
					mu.Lock()
					prevStatus := paneStatus[role]
					waitingCount := paneStatusCount[role]
					seen := seenTokens[role]
					mu.Unlock()
					if strings.Contains(lines, "tokens") {
						if !seen {
							mu.Lock()
							seenTokens[role] = true
							mu.Unlock()
						}
						if prevStatus != "running" {
							mu.Lock()
							paneStatus[role] = "running"
							mu.Unlock()
							fmt.Printf("[DEBUG] %sペインがtokens検知→running状態に遷移\n", role)
						}
					} else {
						if !seen {
							if prevStatus != "init" {
								mu.Lock()
								paneStatus[role] = "init"
								mu.Unlock()
								fmt.Printf("[DEBUG] %sペインは初期化中（tokens未検知）\n", role)
							}
							// tokens未検知の間はinitを維持
							goto WAITLOOP
						}
						if prevStatus != "waiting" {
							mu.Lock()
							paneStatus[role] = "waiting"
							paneStatusCount[role] = waitingCount + 1
							mu.Unlock()
							fmt.Printf("[DEBUG] %sペインがtokens消失→waiting状態に遷移（%d回目）\n", role, waitingCount+1)
						}
					}
				WAITLOOP:
					if !ready[role] || paneStatus[role] != "waiting" || paneStatusCount[role] < 2 {
						time.Sleep(1 * time.Second)
						continue
					}
					executed := loadExecuted()
					// 全出力をスペースで1行に連結し、clampany inqueueコマンド（クォート内も含めて貪欲に）を抽出
					joined := strings.ReplaceAll(lines, "\n", " ")
					re := regexp.MustCompile(`(?s)clampany inqueue \w+ ".+?"`)
					matches := re.FindAllString(joined, -1)
					for _, cmd := range matches {
						if !skippedFirstInqueue[role] {
							mu.Lock()
							skippedFirstInqueue[role] = true
							mu.Unlock()
							fmt.Println("[SKIP] 初回clampany inqueueコマンドをスキップ:", cmd)
							continue
						}
						hash := fmt.Sprintf("%x", sha256.Sum256([]byte(cmd)))
						if !executed[hash] {
							fmt.Println("🟢 実行:", cmd)
							mu.Lock()
							paneStatus[role] = "running"
							mu.Unlock()
							go func(c, h string) {
								if err := exec.Command("sh", "-c", c).Run(); err != nil {
									fmt.Fprintf(os.Stderr, "❌ 実行失敗: %s: %v\n", c, err)
								}
								appendHash(h)
							}(cmd, hash)
						}
					}
					time.Sleep(2 * time.Second)
				}
			}(paneID, role)
		}
	*/

	// ステータスファイル出力用関数
	writeStatus := func() {
		os.MkdirAll("run/latest", 0755)
		f, _ := os.Create("run/latest/pane_status.txt")
		mu.Lock()
		for role, status := range paneStatus {
			f.WriteString(fmt.Sprintf("[%s] %s\n", role, status))
		}
		mu.Unlock()
		f.Close()
	}
	// ステータス監視ペインを追加
	statusPaneCmd := exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "watch -n 1 cat run/latest/pane_status.txt")
	statusPaneCmd.Output()
	// ステータスファイルを定期的に更新
	go func() {
		for {
			writeStatus()
			time.Sleep(1 * time.Second)
		}
	}()

	// 7. Ctrl+Cまで無限待機
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("[Clampany] 終了します")
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
	os.MkdirAll("_clampany/queue", 0755)
	rootCmd.AddCommand(initCmd)
}
