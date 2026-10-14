package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	runtimecmd "github.com/foxford/ai-devcontainer/bin/adc/runtime"
	"github.com/spf13/cobra"
)

func updateCommands(root *cobra.Command) {
	updates := &cobra.Command{Use: "updates", Short: "Показать новые версии скиллов, документов, MCP и бандлов", Args: cobra.NoArgs}
	refresh := updates.Flags().Bool("refresh", false, "Проверить remote сейчас, без ожидания суточного кэша")
	jsonOutput := updates.Flags().Bool("json", false, "Вывести результат в JSON без цвета")
	updates.RunE = func(cmd *cobra.Command, args []string) error {
		return runUpdateCheck(cmd, *refresh, *jsonOutput)
	}
	root.AddCommand(updates)
	update, _, _ := root.Find([]string{"update"})
	update.Short = "Обновить ADC или только доставку (--delivery)"
	delivery := update.Flags().Bool("delivery", false, "Обновить скиллы, документы, инструкции, MCP и бандлы без пересборки и перезапуска")
	check := update.Flags().Bool("check", false, "Проверить обновления без установки и перезапуска workspace")
	checkJSON := update.Flags().Bool("json", false, "JSON результата проверки (с --check)")
	update.RunE = func(cmd *cobra.Command, args []string) error {
		if *delivery {
			if *check || *checkJSON {
				return errors.New("--delivery нельзя совмещать с --check или --json; для проверки используйте adc updates")
			}
			return runDeliveryUpdate(cmd)
		}
		if *check {
			return runUpdateCheck(cmd, true, *checkJSON)
		}
		if *checkJSON {
			return errors.New("--json требует --check")
		}
		if os.Getenv("ADC_RUNTIME") == "1" {
			return errors.New("внутри контейнера используйте adc update --delivery; полное обновление ADC выполняется на хосте")
		}
		return dispatch([]string{"update"})
	}
}

func runDeliveryUpdate(cmd *cobra.Command) error {
	a, err := shared.New()
	if err != nil {
		return err
	}
	// Fetches need more time than the background version check. Cancellation
	// reaches Git; no image build, CLI replacement or container restart occurs.
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()
	fmt.Fprintln(cmd.OutOrStdout(), "Получение доставки…")
	changes, err := a.UpdateDelivery(ctx, func() error {
		if a.Runtime {
			return (*runtimecmd.App)(a).Sync([]string{"--all"})
		}
		return (*host.App)(a).ApplyDelivery()
	})
	if err != nil {
		return err
	}
	return shared.WriteDeliveryChanges(cmd.OutOrStdout(), "ADC: доставка обновлена", changes, shared.ColorEnabled(cmd.OutOrStdout()), true)
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
