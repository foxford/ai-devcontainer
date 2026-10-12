package main

import (
	"fmt"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/spf13/cobra"
)

func completionCommands(root *cobra.Command) {
	root.CompletionOptions.DisableDefaultCmd = true
	completion := &cobra.Command{Use: "completion", Short: "Сохранить автодополнение и показать команду подключения", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
	root.AddCommand(completion)
	stdout := completion.PersistentFlags().Bool("stdout", false, "Вывести полный скрипт вместо сохранения файла")
	noDescriptions := completion.PersistentFlags().Bool("no-descriptions", false, "Не включать описания команд в автодополнение")
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		shell := shell
		completion.AddCommand(&cobra.Command{Use: shell, Short: "Сохранить автодополнение для " + shell, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			data, err := shared.CompletionScript(root, shell, !*noDescriptions)
			if err != nil {
				return err
			}
			if *stdout {
				_, err := cmd.OutOrStdout().Write(data)
				return err
			}
			a, err := shared.New()
			if err != nil {
				return err
			}
			path, err := a.CompletionPath(shell)
			if err != nil {
				return err
			}
			instructions, err := shared.CompletionInstructions(shell, path)
			if err != nil {
				return err
			}
			if err := shared.AtomicWrite(path, data, 0644); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Скрипт сохранён: %s\n%s", path, instructions)
			return err
		}})
	}
}
