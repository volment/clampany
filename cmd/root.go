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
	"strings"
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

var engineerCount int

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
	aiRoles := []string{}
	entries, err := readInstructionDir()
	if err == nil {
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "sufix.md" {
				role := strings.TrimSuffix(entry.Name(), ".md")
				if role == "engineer" && engineerCount > 0 {
					continue
				}
				aiRoles = append(aiRoles, role)
			}
		}
	}
	if engineerCount > 0 {
		for i := 1; i <= engineerCount; i++ {
			aiRoles = append(aiRoles, fmt.Sprintf("engineer%d", i))
		}
	} else {
		for _, entry := range entries {
			if entry.Type().IsRegular() && entry.Name() == "engineer.md" {
				aiRoles = append(aiRoles, "engineer")
			}
		}
	}
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

	// ロール分割
	rightRoles := []string{}
	for _, r := range aiRoles {
		if strings.HasPrefix(r, "engineer") {
			rightRoles = append(rightRoles, r)
		}
	}

	paneMap := map[string]string{}

	// 1. split-window -h（右に分割、2列）
	cmd := exec.Command("tmux", "split-window", "-h", "-P", "-F", "#{pane_id}", "zsh")
	out, err := cmd.Output()
	if err != nil {
		fmt.Println("tmux初期右分割失敗:", err)
		os.Exit(1)
	}
	curPaneCmd := exec.Command("tmux", "display-message", "-p", "#{pane_id}")
	curPaneOut, err := curPaneCmd.Output()
	if err != nil {
		fmt.Println("tmux現在ペイン取得失敗:", err)
		os.Exit(1)
	}
	leftPane := strings.TrimSpace(string(curPaneOut))
	rightPane := strings.TrimSpace(string(out))

	// 2. split-window -h（さらに右に分割、3列）
	cmd = exec.Command("tmux", "split-window", "-h", "-P", "-F", "#{pane_id}", "zsh")
	out, err = cmd.Output()
	if err != nil {
		fmt.Println("tmux中央分割失敗:", err)
		os.Exit(1)
	}

	// 3. select-pane -L（左端に移動）
	exec.Command("tmux", "select-pane", "-L").Run()
	exec.Command("tmux", "select-pane", "-L").Run()

	// 4. split-window -v（左列を下に分割、2ペイン目＝監視用）
	cmd = exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "watch -n 1 cat run/latest/pane_status.txt")
	out, err = cmd.Output()
	if err != nil {
		fmt.Println("tmux左列監視ペイン作成失敗:", err)
		os.Exit(1)
	}
	watchPane := strings.TrimSpace(string(out))
	paneMap["active"] = leftPane
	paneMap["watch"] = watchPane
	// 左列均等割り
	exec.Command("bash", "-c", `left=$(tmux list-panes -F "#{pane_left}" | sort -n | uniq | sed -n 2p); panes=($(tmux list-panes -F "#{pane_id} #{pane_left}" | awk -v l="$left" '$2 == l {print $1}')); h=$(tmux display -p "#{window_height}"); eh=$((h / ${#panes[@]})); for p in "${panes[@]}"; do tmux resize-pane -t "$p" -y "$eh"; done`).Run()

	// 5. select-pane -R（中央列へ移動）
	exec.Command("tmux", "select-pane", "-R").Run()

	// 6. ceo起動
	// 7. split-window -v（中央列下に分割、2ペイン目）
	cmd = exec.Command("tmux", "display-message", "-p", "#{pane_id}")
	out, err = cmd.Output()
	if err != nil {
		fmt.Println("tmux中央列ceo分割失敗:", err)
		os.Exit(1)
	}
	centerPane := strings.TrimSpace(string(out))
	paneMap["ceo"] = centerPane
	// claude起動・ラベル付与
	exec.Command("tmux", "select-pane", "-t", centerPane, "-T", "ceo").Run()
	roleContent, _ := readInstructionFile("ceo.md")
	sufixContent, _ := readInstructionFile("sufix.md")
	prompt := strings.TrimSpace(string(roleContent) + "\n" + string(sufixContent))
	prompt = strings.ReplaceAll(prompt, "'", "'\\''")
	tmpfile, _ := os.CreateTemp("", "clampany_prompt_*.txt")
	defer os.Remove(tmpfile.Name())
	tmpfile.WriteString(prompt)
	tmpfile.Close()
	cmdStr := fmt.Sprintf("claude --dangerously-skip-permissions \"$(cat %s)\"", tmpfile.Name())
	exec.Command("tmux", "send-keys", "-t", centerPane, cmdStr, "C-m").Run()
	time.Sleep(800 * time.Millisecond)
	for _, line := range strings.Split(string(roleContent), "\n") {
		if strings.TrimSpace(line) != "" {
			exec.Command("tmux", "send-keys", "-t", centerPane, line, "C-m").Run()
			exec.Command("tmux", "send-keys", "-t", centerPane, "Enter").Run()
			time.Sleep(80 * time.Millisecond)
		}
	}
	for _, line := range strings.Split(string(sufixContent), "\n") {
		if strings.TrimSpace(line) != "" {
			exec.Command("tmux", "send-keys", "-t", centerPane, line, "C-m").Run()
			exec.Command("tmux", "send-keys", "-t", centerPane, "Enter").Run()
			time.Sleep(80 * time.Millisecond)
		}
	}

	// 8. split-window -v（中央列下に分割、2ペイン目）
	cmd = exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "zsh")
	out, err = cmd.Output()
	if err != nil {
		fmt.Println("tmux中央列pm分割失敗:", err)
		os.Exit(1)
	}
	pmPane := strings.TrimSpace(string(out))
	paneMap["pm"] = pmPane
	// claude起動・ラベル付与
	exec.Command("tmux", "select-pane", "-t", pmPane, "-T", "pm").Run()
	roleContent, _ = readInstructionFile("pm.md")
	sufixContent, _ = readInstructionFile("sufix.md")
	prompt = strings.TrimSpace(string(roleContent) + "\n" + string(sufixContent))
	prompt = strings.ReplaceAll(prompt, "'", "'\\''")
	tmpfile, _ = os.CreateTemp("", "clampany_prompt_*.txt")
	defer os.Remove(tmpfile.Name())
	tmpfile.WriteString(prompt)
	tmpfile.Close()
	cmdStr = fmt.Sprintf("claude --dangerously-skip-permissions \"$(cat %s)\"", tmpfile.Name())
	exec.Command("tmux", "send-keys", "-t", pmPane, cmdStr, "C-m").Run()
	time.Sleep(800 * time.Millisecond)
	for _, line := range strings.Split(string(roleContent), "\n") {
		if strings.TrimSpace(line) != "" {
			exec.Command("tmux", "send-keys", "-t", pmPane, line, "C-m").Run()
			exec.Command("tmux", "send-keys", "-t", pmPane, "Enter").Run()
			time.Sleep(80 * time.Millisecond)
		}
	}
	for _, line := range strings.Split(string(sufixContent), "\n") {
		if strings.TrimSpace(line) != "" {
			exec.Command("tmux", "send-keys", "-t", pmPane, line, "C-m").Run()
			exec.Command("tmux", "send-keys", "-t", pmPane, "Enter").Run()
			time.Sleep(80 * time.Millisecond)
		}
	}

	// 8. （中央列下に分割、3ペイン目）
	cmd = exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "zsh")
	out, err = cmd.Output()
	if err != nil {
		fmt.Println("tmux中央列planner分割失敗:", err)
		os.Exit(1)
	}
	plannerPane := strings.TrimSpace(string(out))
	paneMap["planner"] = plannerPane
	// claude起動・ラベル付与
	exec.Command("tmux", "select-pane", "-t", plannerPane, "-T", "planner").Run()
	roleContent, _ = readInstructionFile("planner.md")
	sufixContent, _ = readInstructionFile("sufix.md")
	prompt = strings.TrimSpace(string(roleContent) + "\n" + string(sufixContent))
	prompt = strings.ReplaceAll(prompt, "'", "'\\''")
	tmpfile, _ = os.CreateTemp("", "clampany_prompt_*.txt")
	defer os.Remove(tmpfile.Name())
	tmpfile.WriteString(prompt)
	tmpfile.Close()
	cmdStr = fmt.Sprintf("claude --dangerously-skip-permissions \"$(cat %s)\"", tmpfile.Name())
	exec.Command("tmux", "send-keys", "-t", plannerPane, cmdStr, "C-m").Run()
	time.Sleep(800 * time.Millisecond)
	for _, line := range strings.Split(string(roleContent), "\n") {
		if strings.TrimSpace(line) != "" {
			exec.Command("tmux", "send-keys", "-t", plannerPane, line, "C-m").Run()
			exec.Command("tmux", "send-keys", "-t", plannerPane, "Enter").Run()
			time.Sleep(80 * time.Millisecond)
		}
	}
	for _, line := range strings.Split(string(sufixContent), "\n") {
		if strings.TrimSpace(line) != "" {
			exec.Command("tmux", "send-keys", "-t", plannerPane, line, "C-m").Run()
			exec.Command("tmux", "send-keys", "-t", plannerPane, "Enter").Run()
			time.Sleep(80 * time.Millisecond)
		}
	}

	// 9. select-pane -R（右列へ移動）
	exec.Command("tmux", "select-pane", "-R").Run()

	// 移動後のアクティブペインIDを取得
	out, err = exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		panic("tmux pane ID取得失敗")
	}

	// 10. engineerN起動
	rightPaneIDs := []string{}
	rightPane = strings.TrimSpace(string(out))
	rightCurPane := rightPane
	for i, role := range rightRoles {
		if i == 0 {
			// 1つ目は既存ペイン
			rightPaneIDs = append(rightPaneIDs, rightCurPane)
			paneMap[role] = rightCurPane
			// claude起動・ラベル付与
			exec.Command("tmux", "select-pane", "-t", rightCurPane, "-T", role).Run()
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
			exec.Command("tmux", "send-keys", "-t", rightCurPane, cmdStr, "C-m").Run()
			time.Sleep(800 * time.Millisecond)
			for _, line := range strings.Split(string(roleContent), "\n") {
				if strings.TrimSpace(line) != "" {
					exec.Command("tmux", "send-keys", "-t", rightCurPane, line, "C-m").Run()
					exec.Command("tmux", "send-keys", "-t", rightCurPane, "Enter").Run()
					time.Sleep(80 * time.Millisecond)
				}
			}
			for _, line := range strings.Split(string(sufixContent), "\n") {
				if strings.TrimSpace(line) != "" {
					exec.Command("tmux", "send-keys", "-t", rightCurPane, line, "C-m").Run()
					exec.Command("tmux", "send-keys", "-t", rightCurPane, "Enter").Run()
					time.Sleep(80 * time.Millisecond)
				}
			}
		} else {
			// 2つ目以降は新規ペイン
			cmd = exec.Command("tmux", "split-window", "-v", "-P", "-F", "#{pane_id}", "zsh")
			out, err = cmd.Output()
			if err != nil {
				fmt.Println("tmux右列分割失敗:", err)
				os.Exit(1)
			}
			rightCurPane = strings.TrimSpace(string(out))
			rightPaneIDs = append(rightPaneIDs, rightCurPane)
			paneMap[role] = rightCurPane
			// claude起動・ラベル付与
			exec.Command("tmux", "select-pane", "-t", rightCurPane, "-T", role).Run()
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
			exec.Command("tmux", "send-keys", "-t", rightCurPane, cmdStr, "C-m").Run()
			time.Sleep(800 * time.Millisecond)
			for _, line := range strings.Split(string(roleContent), "\n") {
				if strings.TrimSpace(line) != "" {
					exec.Command("tmux", "send-keys", "-t", rightCurPane, line, "C-m").Run()
					exec.Command("tmux", "send-keys", "-t", rightCurPane, "Enter").Run()
					time.Sleep(80 * time.Millisecond)
				}
			}
			for _, line := range strings.Split(string(sufixContent), "\n") {
				if strings.TrimSpace(line) != "" {
					exec.Command("tmux", "send-keys", "-t", rightCurPane, line, "C-m").Run()
					exec.Command("tmux", "send-keys", "-t", rightCurPane, "Enter").Run()
					time.Sleep(80 * time.Millisecond)
				}
			}
		}
	}
	// 右列均等割り
	exec.Command("bash", "-c", `left=$(tmux list-panes -F "#{pane_left}" | sort -n | uniq | sed -n 2p); panes=($(tmux list-panes -F "#{pane_id} #{pane_left}" | awk -v l="$left" '$2 == l {print $1}')); h=$(tmux display -p "#{window_height}"); eh=$((h / ${#panes[@]})); for p in "${panes[@]}"; do tmux resize-pane -t "$p" -y "$eh"; done`).Run()

	// 5. panes.json保存
	os.MkdirAll("run/latest", 0755)
	f, _ := os.Create("run/latest/panes.json")
	json.NewEncoder(f).Encode(paneMap)
	f.Close()

	fmt.Println("[Clampany] 全ロール永続ワーカー起動中。Ctrl+Cで終了")

	// 6. 各ロールごとに<role>_queue.mdを監視し、指示を自分のキューに流し込む
	queues := map[string]chan string{}
	for _, role := range aiRoles {
		queues[role] = make(chan string, 100)
	}

	// 7. 各ロールごとに永続ワーカー起動
	for _, role := range aiRoles {
		go func(role string) {
			execAI := &executor.AIExecutor{PaneID: paneMap[role]}
			for prompt := range queues[role] {
				execAI.Execute(prompt)
			}
		}(role)
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
		// paneStatusの参照を削除
		// for role, status := range paneStatus {
		// 	f.WriteString(fmt.Sprintf("[%s] %s\n", role, status))
		// }
		f.Close()
	}
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
	rootCmd.PersistentFlags().IntVar(&engineerCount, "engineer", 0, "追加するengineerロールの数 (例: --engineer 3 でengineer1,engineer2,engineer3)")
}
