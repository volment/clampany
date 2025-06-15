package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
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
