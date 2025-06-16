package cmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var inqueueMutex sync.Mutex
var inqueueCounter = map[string]int{}

var inqueueCmd = &cobra.Command{
	Use:   "inqueue <role> <message>",
	Short: "指定ロールの指示を<role>_queue.mdに追記",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		role := args[0]
		message := args[1]

		// ロールの上下関係を定義
		allowedDown := map[string]string{
			"ceo":     "pm",
			"pm":      "planner",
			"planner": "engineer",
		}
		allowedUp := map[string]string{
			"pm":       "ceo",
			"planner":  "pm",
			"engineer": "planner",
		}

		// メッセージの送り元ロールを推定（現状はコマンド実行者のロール情報がないため、ここは仮実装。必要なら引数追加）
		// ここではmessage内に "from:<role>" のような記述があればそれを使う例
		fromRole := ""
		if strings.Contains(message, "from:") {
			parts := strings.Split(message, "from:")
			if len(parts) > 1 {
				fromRole = strings.Fields(parts[1])[0]
			}
		}
		// fromRoleがなければ許可（従来通り）
		if fromRole != "" {
			ok := false
			if allowedDown[fromRole] == role {
				ok = true // トップダウン
			}
			if allowedUp[fromRole] == role {
				ok = true // ボトムアップ
			}
			if !ok {
				// ペインに警告送信
				// run/latest/panes.jsonからペインID取得
				f, err := os.Open("run/latest/panes.json")
				if err == nil {
					defer f.Close()
					var paneMap map[string]string
					if err := json.NewDecoder(f).Decode(&paneMap); err == nil {
						paneID, ok := paneMap[fromRole]
						if ok {
							msg := fmt.Sprintf("あなたは依頼の場合は`%s role`、問い合わせの場合は`%s role`にしか送信できません。", allowedDown[fromRole], allowedUp[fromRole])
							exec.Command("tmux", "send-keys", "-t", paneID, msg, "C-m").Run()
						}
					}
				}
				return
			}
		}

		// roles.yamlがなくてもエラーにしない。instructions/や*_queue.mdからロール候補を自動検出
		var candidates []string
		// instructions/から（embed.FSを使う）
		entries, err := readInstructionDir()
		if err == nil {
			for _, entry := range entries {
				if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "sufix.md" {
					roleName := strings.TrimSuffix(entry.Name(), ".md")
					if strings.HasPrefix(roleName, role) {
						candidates = append(candidates, roleName)
					}
				}
			}
		}
		// *_queue.mdからも
		queueEntries, err := os.ReadDir(".")
		for _, entry := range queueEntries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), "_queue.md") {
				roleName := strings.TrimSuffix(entry.Name(), "_queue.md")
				if strings.HasPrefix(roleName, role) {
					found := false
					for _, r := range candidates {
						if r == roleName {
							found = true
							break
						}
					}
					if !found {
						candidates = append(candidates, roleName)
					}
				}
			}
		}
		if len(candidates) == 0 {
			fmt.Printf("ロール %s が見つかりません\n", role)
			os.Exit(1)
		}
		// ラウンドロビンで割り当て
		inqueueMutex.Lock()
		idx := inqueueCounter[role] % len(candidates)
		inqueueCounter[role]++
		inqueueMutex.Unlock()
		assigned := candidates[idx]
		queueFile := fmt.Sprintf("_clampany/queue/%s_queue_%s.md", role, fmt.Sprintf("%x", sha256.Sum256([]byte(message)))[:8])
		// 改行をスペースに置換して1行にまとめる
		message = strings.ReplaceAll(message, "\n", " ")
		message = strings.ReplaceAll(message, "\r", " ")
		section := message + "\n"
		if err := os.WriteFile(queueFile, []byte(section), 0644); err != nil {
			fmt.Println(queueFile+"書き込み失敗:", err)
			os.Exit(1)
		}
		fmt.Printf("[INQUEUE] %s → %s (%s)\n", assigned, message, queueFile)
	},
}

func init() {
	rootCmd.AddCommand(inqueueCmd)
}
