package main

import (
	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/spf13/cobra"
)

func legacyMigrationCommands(migrate *cobra.Command) {
	group := &cobra.Command{Use: "legacy", Short: "Сохранить v0 и импортировать данные в отдельный новый workspace"}
	migrate.AddCommand(group)
	app := func() (*host.App, error) {
		a, err := shared.New()
		return (*host.App)(a), err
	}
	plan := &cobra.Command{Use: "plan", Short: "Прочитать маунты старых контейнеров, не изменяя данные", Args: cobra.NoArgs}
	containers := plan.Flags().StringSlice("container", nil, "Старые devcontainer: имя или ID; флаг можно повторять")
	output := plan.Flags().String("output", "", "Записать JSON-план в файл вместо stdout")
	plan.RunE = func(_ *cobra.Command, _ []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyPlan(*containers, *output)
	}
	group.AddCommand(plan)
	backup := &cobra.Command{Use: "backup <plan.json>", Short: "Архивировать все постоянные маунты и writable layers остановленных контейнеров", Args: cobra.ExactArgs(1)}
	destination := backup.Flags().String("output", "", "Новый каталог резервной копии вне исходных маунтов")
	_ = backup.MarkFlagRequired("output")
	backup.RunE = func(_ *cobra.Command, args []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyBackup(args[0], *destination)
	}
	group.AddCommand(backup)
	verify := &cobra.Command{Use: "verify <backup-directory>", Short: "Проверить полноту и SHA-256 всех архивов", Args: cobra.ExactArgs(1)}
	verify.RunE = func(_ *cobra.Command, args []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyVerify(args[0])
	}
	group.AddCommand(verify)
	cleanup := &cobra.Command{Use: "cleanup <project|path>", Short: "Показать и убрать доставленные файлы ADC с проверенной резервной копией", Args: cobra.ExactArgs(1)}
	apply := cleanup.Flags().Bool("apply", false, "После проверенной копии удалить показанные файлы; без флага только предпросмотр")
	devcontainer := cleanup.Flags().Bool("devcontainer", false, "Включить весь проектный .devcontainer в очистку и резервную копию")
	cleanup.RunE = func(_ *cobra.Command, args []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyCleanup(args[0], *apply, *devcontainer)
	}
	group.AddCommand(cleanup)
	finish := &cobra.Command{Use: "finish", Short: "Завершить регистрацию после успешно скопированных данных", Args: cobra.NoArgs}
	finish.RunE = func(_ *cobra.Command, _ []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyFinish()
	}
	group.AddCommand(finish)
	importCmd := &cobra.Command{Use: "import <backup-directory>", Short: "Импортировать профили, историю и общий MCP в новый workspace", Args: cobra.ExactArgs(1)}
	account := importCmd.Flags().String("account", "", "Проект, чьи аккаунты и настройки станут общими")
	workspace := importCmd.Flags().String("workspace", "", "Имя отдельного нового workspace; существующие тома не перезаписываются")
	projects := importCmd.Flags().StringArray("project", nil, "Заменить host path проекта: project-id=/host/path")
	_ = importCmd.MarkFlagRequired("account")
	importCmd.RunE = func(_ *cobra.Command, args []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyImport(args[0], *account, *workspace, *projects)
	}
	group.AddCommand(importCmd)
	hostImport := &cobra.Command{Use: "import-host <plan.json>", Short: "Скопировать примонтированные профили в общий workspace; код и кеши остаются на хосте", Args: cobra.ExactArgs(1)}
	hostAccount := hostImport.Flags().String("account", "", "Проект, чьи аккаунты и настройки станут общими")
	hostWorkspace := hostImport.Flags().String("workspace", "", "Имя новой среды без существующих томов")
	hostProjects := hostImport.Flags().StringArray("project", nil, "Заменить путь проекта: project-id=/host/path")
	_ = hostImport.MarkFlagRequired("account")
	hostImport.RunE = func(_ *cobra.Command, args []string) error {
		a, err := app()
		if err != nil {
			return err
		}
		return a.LegacyImportHost(args[0], *hostAccount, *hostWorkspace, *hostProjects)
	}
	group.AddCommand(hostImport)
}
