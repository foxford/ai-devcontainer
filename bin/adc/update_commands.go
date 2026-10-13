package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/spf13/cobra"
)

func updateCommands(root *cobra.Command) {
	updates := &cobra.Command{Use: "updates", Short: "Показать доступные версии skills, MCP и бандлов", Args: cobra.NoArgs}
	refresh := updates.Flags().Bool("refresh", false, "Проверить remote сейчас, без ожидания суточного кэша")
	jsonOutput := updates.Flags().Bool("json", false, "Вывести результат в JSON без цвета")
	updates.RunE = func(cmd *cobra.Command, args []string) error {
		return runUpdateCheck(cmd, *refresh, *jsonOutput)
	}
	root.AddCommand(updates)
	update, _, _ := root.Find([]string{"update"})
	check := update.Flags().Bool("check", false, "Проверить обновления без установки и перезапуска workspace")
	checkJSON := update.Flags().Bool("json", false, "JSON результата проверки (с --check)")
	update.RunE = func(cmd *cobra.Command, args []string) error {
		if *check {
			return runUpdateCheck(cmd, true, *checkJSON)
		}
		if *checkJSON {
			return errors.New("--json требует --check")
		}
		return dispatch([]string{"update"})
	}
}

func runUpdateCheck(cmd *cobra.Command, force, jsonOutput bool) error {
	a, err := shared.New()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()
	status, checkErr := a.CheckUpdates(ctx, force)
	if checkErr != nil && status.Error == "" {
		status.Error = checkErr.Error()
	}
	out := cmd.OutOrStdout()
	if jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(status); err != nil {
			return err
		}
	} else {
		color := shared.ColorEnabled(out)
		if err := shared.WriteUpdateStatus(out, status, color); err != nil {
			return err
		}
	}
	return checkErr
}
