package cmd

import (
	"clampany/internal/executor"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

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

// --- ステータス管理用グローバル変数 ---
var (
	mu              sync.Mutex
	paneStatus      = map[string]string{} // ロールごとの状態: init/waiting/running
	paneStatusCount = map[string]int{}    // ロールごとのwaiting回数
	currentCommand  = map[string]string{} // ロールごとの現在のコマンド
	runningCount    = map[string]int{}    // running回数
	waitingCount    = map[string]int{}    // waiting回数
)

var aiRoles []string // ←グローバルに移動

// 埋め込み→外部ファイルの順で読む関数
func readInstructionFile(name string) ([]byte, error) {
	return os.ReadFile("_clampany/instructions/" + name)
}

// ディレクトリ一覧も埋め込み→外部ファイル順で取得
func readInstructionDir() ([]fs.DirEntry, error) {
	return os.ReadDir("_clampany/instructions")
}

// --- 追加: ロールごとのclaudeコマンド生成 ---
func getClaudeCommand(role string) string {
	var inst string
	switch role {
	case "ceo":
		inst = "ceo.md"
	case "pm":
		inst = "pm.md"
	case "planner":
		inst = "planner.md"
	default:
		inst = "engineer.md"
	}
	return fmt.Sprintf(`claude --dangerously-skip-permissions "$(cat _clampany/instructions/%s _clampany/instructions/sufix.md)"`, inst)
}

// --- 追加: tmuxペイン生成とコマンド送信 ---
func createRolePane(role, label, splitDir string, isFirst bool, basePane string) (string, error) {
	var paneID string
	var err error
	if isFirst {
		paneID = basePane
	} else {
		cmd := exec.Command("tmux", "split-window", splitDir, "-P", "-F", "#{pane_id}", "zsh")
		out, err2 := cmd.Output()
		if err2 != nil {
			return "", err2
		}
		paneID = strings.TrimSpace(string(out))
	}
	exec.Command("tmux", "select-pane", "-t", paneID, "-T", label).Run()

	cmdStr := getClaudeCommand(role)

	// send-keys に渡すときはクォートで囲むと安全
	err = exec.Command("tmux", "send-keys", "-t", paneID, cmdStr, "C-m").Run()
	if err != nil {
		log.Printf("tmux send-keys failed: %v", err)
	}
	return paneID, err
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
	aiRoles = []string{} // ←ここで初期化
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

	// --- ここで全ロールのステータス初期化 ---
	for _, role := range aiRoles {
		mu.Lock()
		paneStatus[role] = "init"
		currentCommand[role] = ""
		runningCount[role] = 0
		waitingCount[role] = 0
		mu.Unlock()
	}

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
	createRolePane("ceo", "ceo", "-v", true, centerPane)
	time.Sleep(800 * time.Millisecond)

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
	createRolePane("pm", "pm", "-v", true, pmPane)

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
	createRolePane("planner", "planner", "-v", true, plannerPane)

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
		isFirst := (i == 0)
		pane, err := createRolePane(role, role, "-v", isFirst, rightCurPane)
		if err != nil {
			fmt.Printf("tmux右列分割失敗: %v\n", err)
			os.Exit(1)
		}
		rightPaneIDs = append(rightPaneIDs, pane)
		paneMap[role] = pane
		if !isFirst {
			rightCurPane = pane
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

	// --- 追加: _clampany/queue/<role>_queue*.md を監視し、内容をチャネルに流し込む ---
	for _, role := range aiRoles {
		if strings.HasPrefix(role, "engineer") {
			continue
		}
		go func(role string) {
			fileSizes := map[string]int64{}
			pendingLines := []string{}
			for {
				pattern := fmt.Sprintf("_clampany/queue/%s_queue*.md", role)
				files, err := filepath.Glob(pattern)
				if err == nil {
					for _, queueFile := range files {
						fi, err := os.Stat(queueFile)
						if err == nil {
							lastSize := fileSizes[queueFile]
							curSize := fi.Size()
							if _, ok := fileSizes[queueFile]; !ok || curSize > fileSizes[queueFile] {
								f, err := os.Open(queueFile)
								if err == nil {
									f.Seek(lastSize, io.SeekStart)
									buf, _ := io.ReadAll(f)
									lines := strings.Split(string(buf), "\n")
									for _, line := range lines {
										line = strings.TrimSpace(line)
										if line != "" {
											pendingLines = append(pendingLines, line)
										}
									}
									fileSizes[queueFile] = fi.Size()
									f.Close()
									os.Remove(queueFile)
								}
							}
							fileSizes[queueFile] = curSize
						}
					}
				}
				mu.Lock()
				status := paneStatus[role]
				mu.Unlock()
				if status == "waiting" && len(pendingLines) > 0 {
					queues[role] <- pendingLines[0]
					pendingLines = pendingLines[1:]
				}
				time.Sleep(1 * time.Second)
			}
		}(role)
	}

	// --- engineer専用の共通キュー監視 ---
	go func() {
		fileSizes := map[string]int64{}
		var pendingLines []string
		for {
			pattern := "_clampany/queue/engineer_queue*.md"
			files, err := filepath.Glob(pattern)
			if err == nil {
				for _, queueFile := range files {
					fi, err := os.Stat(queueFile)
					if err == nil {
						lastSize := fileSizes[queueFile]
						if fi.Size() > lastSize {
							f, err := os.Open(queueFile)
							if err == nil {
								f.Seek(lastSize, io.SeekStart)
								buf, _ := io.ReadAll(f)
								lines := strings.Split(string(buf), "\n")
								for _, line := range lines {
									line = strings.TrimSpace(line)
									if line != "" {
										pendingLines = append(pendingLines, line)
									}
								}
								fileSizes[queueFile] = fi.Size()
								f.Close()
								os.Remove(queueFile)
							}
						}
					}
				}
			}

			newPending := []string{}
			for _, line := range pendingLines {
				assigned := false

				mu.Lock()
				for _, r := range aiRoles {
					if strings.HasPrefix(r, "engineer") && paneStatus[r] == "waiting" {
						queues[r] <- line
						assigned = true
						break
					}
				}
				mu.Unlock()

				if !assigned {
					newPending = append(newPending, line)
				}
			}

			// pendingLinesを置き換え
			pendingLines = newPending

			time.Sleep(1 * time.Second)
		}
	}()

	// 7. 各ロールごとに永続ワーカー起動
	for _, role := range aiRoles {
		go func(role string) {
			execAI := &executor.AIExecutor{PaneID: paneMap[role]}
			for prompt := range queues[role] {
				mu.Lock()
				currentCommand[role] = prompt
				paneStatus[role] = "running"
				runningCount[role]++
				mu.Unlock()
				execAI.Execute(prompt)
				mu.Lock()
				paneStatus[role] = "waiting"
				waitingCount[role]++
				currentCommand[role] = ""
				mu.Unlock()
			}
		}(role)
	}

	// --- 追加: 各ワーカーの標準出力を監視し、[READY]が出力されたらinit→waitingに遷移 ---
	for _, role := range aiRoles {
		paneID := paneMap[role]
		go func(role, paneID string) {
			for {
				out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-S", "-1000").Output()
				if err == nil {
					lines := strings.Split(string(out), "\n")
					for _, line := range lines {
						cleanLine := ansiRegexp.ReplaceAllString(line, "")
						if strings.Contains(cleanLine, "[READY]") {
							mu.Lock()
							if paneStatus[role] == "init" {
								paneStatus[role] = "waiting"
							}
							mu.Unlock()
						}
					}
				}
				time.Sleep(1 * time.Second)

				// すでにwaitingになっていたら終了
				mu.Lock()
				if paneStatus[role] == "waiting" {
					mu.Unlock()
					break
				}
				mu.Unlock()
			}
		}(role, paneID)
	}

	// --- 追加: tokens表示中はrunning, それ以外はwaitingに遷移 ---
	for _, role := range aiRoles {
		paneID := paneMap[role]
		go func(role, paneID string) {
			for {
				out, err := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-S", "-1000").Output()
				if err == nil {
					lines := strings.Split(string(out), "\n")
					foundTokens := false
					for _, line := range lines {
						cleanLine := ansiRegexp.ReplaceAllString(line, "")
						if strings.Contains(cleanLine, "tokens") {
							foundTokens = true
							break
						}
					}
					mu.Lock()
					if foundTokens {
						if paneStatus[role] != "running" {
							paneStatus[role] = "running"
						}
					} else {
						if paneStatus[role] == "running" {
							paneStatus[role] = "waiting"
						}
					}
					mu.Unlock()
				}
				time.Sleep(1 * time.Second)
			}
		}(role, paneID)
	}

	// ステータスファイル出力用関数
	writeStatus := func() {
		os.MkdirAll("run/latest", 0755)
		f, _ := os.Create("run/latest/pane_status.txt")
		defer f.Close()

		// ロール順固定: aiRolesの順番で出力
		roles := aiRoles

		for _, role := range roles {
			mu.Lock()
			status := paneStatus[role]
			cmd := currentCommand[role]
			runCnt := runningCount[role]
			waitCnt := waitingCount[role]
			mu.Unlock()

			// 稼働率計算
			total := runCnt + waitCnt
			var rate int
			if status == "waiting" {
				rate = 0
			} else if total > 0 {
				rate = int(float64(runCnt) / float64(total) * 100)
			} else {
				rate = 0
			}
			barLen := 10
			barFill := int(float64(rate) / 100 * float64(barLen))
			bar := strings.Repeat("█", barFill) + strings.Repeat("░", barLen-barFill)

			// 状態アイコン
			icon := "⚪"
			switch status {
			case "running":
				icon = "🟢"
			case "waiting":
				icon = "🟡"
			case "init":
				icon = "⚪"
			}

			// コマンド表示
			cmdDisp := cmd
			if cmdDisp == "" {
				if status == "init" {
					cmdDisp = "(初期化中)"
				} else {
					cmdDisp = "(待機中)"
				}
			}

			fmt.Fprintf(f, "[%-9s]%s %-8s | コマンド: %-20s | 稼働率: %s %3d%%\n", role, icon, status, cmdDisp, bar, rate)
		}
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

// コマンド行だけ抽出する関数を追加
func extractCommandLines(content []byte) []string {
	var cmds []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "./clampany inqueue") {
			cmds = append(cmds, line)
		}
	}
	return cmds
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
