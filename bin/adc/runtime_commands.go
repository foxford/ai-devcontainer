package main

import (
	"encoding/json"
	"os"

	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	runtimecmd "github.com/foxford/ai-devcontainer/bin/adc/runtime"
	"github.com/spf13/cobra"
)

func executeMCP(operation runtimecmd.MCPOperation) error {
	data, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	ctx, err := shared.New()
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if ctx.Runtime {
		return (*runtimecmd.App)(ctx).Tool(dir, []string{"mcp", string(data)})
	}
	return (*host.App)(ctx).Exec(dir, []string{"adc", "runtime-tool", "mcp", string(data)}, true)
}

func runtimeCommands(root *cobra.Command) {
	group := func(name, short string) *cobra.Command {
		cmd := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() }}
		root.AddCommand(cmd)
		return cmd
	}
	skills := group("skill", "Скиллы проекта: платформа, форки и обновления")
	for _, spec := range []struct {
		use, short string
		args       cobra.PositionalArgs
	}{
		{"list", "Показать источники скиллов", cobra.NoArgs},
		{"status", "Сверить платформу с базой форков", cobra.NoArgs},
		{"fork <skill> [files...]", "Скопировать выбранные файлы в .agents/skills", cobra.MinimumNArgs(1)},
		{"unfork <skill> [files...]", "Удалить выбранные форки или весь форк скилла", cobra.MinimumNArgs(1)},
		{"migrate", "Убрать совпадающие копии, сохранив проектные изменения", cobra.NoArgs},
		{"sync", "Пересобрать скиллы и контекст", cobra.NoArgs},
	} {
		spec := spec
		cmd := &cobra.Command{Use: spec.use, Short: spec.short, Args: spec.args, RunE: func(cmd *cobra.Command, args []string) error {
			return dispatch(append([]string{"skill", cmd.Name()}, args...))
		}}
		skills.AddCommand(cmd)
	}
	plans := &cobra.Command{Use: "plans", Short: "Показать задачи текущего проекта", Args: cobra.NoArgs}
	all := plans.Flags().Bool("all", false, "Включить завершённые и отменённые задачи")
	plans.RunE = func(cmd *cobra.Command, args []string) error {
		argv := []string{"plans"}
		if *all {
			argv = append(argv, "--all")
		}
		return dispatch(argv)
	}
	root.AddCommand(plans)
	mcp := group("mcp", "MCP проекта: выбранные серверы и четыре слоя настроек")
	for _, spec := range []struct {
		use, short string
		args       cobra.PositionalArgs
	}{
		{"list", "Показать активные серверы, источники и условия", cobra.NoArgs},
		{"add [name] [-- command args...]", "Добавить HTTP/stdio сервер или JSON сниппет", cobra.ArbitraryArgs},
		{"rm <name>", "Удалить сервер из редактируемого слоя", cobra.ExactArgs(1)},
		{"enable <name>", "Включить сервер и запросить недостающий секрет", cobra.ExactArgs(1)},
		{"disable <name>", "Выключить сервер через null в выбранном слое", cobra.ExactArgs(1)},
		{"sync", "Доставить MCP в конфиги агентов", cobra.NoArgs},
		{"fix-secrets", "Вынести литеральные секреты из JSON в локальный env-файл", cobra.NoArgs},
	} {
		spec := spec
		cmd := &cobra.Command{Use: spec.use, Short: spec.short, Args: spec.args}
		operation := runtimecmd.MCPOperation{}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			operation.Action = cmd.Name()
			if cmd.Name() == "add" {
				boundary := cmd.ArgsLenAtDash()
				if boundary >= 0 {
					operation.Command = append([]string{}, args[boundary:]...)
					args = args[:boundary]
				}
				if len(args) > 1 {
					return cobra.MaximumNArgs(1)(cmd, args)
				}
			}
			if len(args) > 0 {
				operation.Name = args[0]
			}
			for _, scope := range []string{"global", "user", "project", "local"} {
				if cmd.Flags().Changed(scope) {
					operation.Scope = scope
				}
			}
			return executeMCP(operation)
		}
		if spec.use != "list" && spec.use != "sync" {
			for _, scope := range []string{"global", "user", "project", "local"} {
				cmd.Flags().Bool(scope, false, "Выбрать слой "+scope)
			}
			cmd.MarkFlagsMutuallyExclusive("global", "user", "project", "local")
		}
		if spec.use == "list" {
			cmd.Flags().BoolVar(&operation.JSON, "json", false, "Вывести план в JSON без значений секретов")
		}
		if spec.use == "add [name] [-- command args...]" {
			cmd.Flags().StringVar(&operation.Definition, "json", "", "Определение сервера или сниппет mcpServers")
			cmd.Flags().StringVar(&operation.URL, "url", "", "URL HTTP MCP сервера")
			cmd.Flags().StringArrayVar(&operation.Headers, "header", nil, "HTTP-заголовок в формате Имя: значение")
			cmd.MarkFlagsMutuallyExclusive("json", "url")
		}
		mcp.AddCommand(cmd)
	}
	internal := &cobra.Command{Use: "runtime-tool <tool> <payload>", Hidden: true, Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return dispatch(append([]string{"runtime-tool"}, args...))
	}}
	root.AddCommand(internal)
}
