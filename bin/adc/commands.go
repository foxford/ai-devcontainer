package main

import (
	"errors"
	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/spf13/cobra"
	"sort"
	"strconv"
	"strings"
)

func Run(args []string) error {
	root := commandTree()
	root.SetArgs(args)
	return root.Execute()
}

// Commands and flags are the only source of help and shell completion. Business
// operations do not print usage and never parse a user's shell command string.
func commandTree() *cobra.Command {
	root := &cobra.Command{Use: "adc", Short: "Общая изолированная среда разработчика", SilenceErrors: true, SilenceUsage: true}
	root.Version = "0.1.0"
	leaf := func(parent *cobra.Command, use, short string, n cobra.PositionalArgs, prefix ...string) *cobra.Command {
		c := &cobra.Command{Use: use, Short: short, Args: n, RunE: func(cmd *cobra.Command, args []string) error {
			return dispatch(append(append([]string{}, prefix...), args...))
		}}
		parent.AddCommand(c)
		return c
	}
	install := leaf(root, "install", "Установить Docker, Orca, PATH и completion", cobra.NoArgs, "install")
	noOrca := install.Flags().Bool("no-orca", false, "Пропустить установку клиента Orca")
	noShell := install.Flags().Bool("no-shell", false, "Оставить PATH и профили оболочек без изменений для параллельной установки")
	install.RunE = func(c *cobra.Command, args []string) error {
		a, e := shared.New()
		if e != nil {
			return e
		}
		return (*host.App)(a).Install(*noOrca, *noShell, root)
	}
	workspace := &cobra.Command{Use: "workspace", Short: "Управление общей Docker средой", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error { return c.Help() }}
	root.AddCommand(workspace)
	init := leaf(workspace, "init [name]", "Создать workspace без изменения проектов", cobra.MaximumNArgs(1), "workspace", "init")
	port := init.Flags().Int("port", 6768, "Порт Orca на 127.0.0.1")
	omniPort := init.Flags().Int("omniroute-port", 20128, "Порт OmniRoute на 127.0.0.1")
	graphPort := init.Flags().Int("graph-port", 4242, "Порт просмотра графа на 127.0.0.1")
	init.RunE = func(c *cobra.Command, args []string) error {
		return dispatch(append(append([]string{"workspace", "init"}, args...), "--port", strconv.Itoa(*port), "--omniroute-port", strconv.Itoa(*omniPort), "--graph-port", strconv.Itoa(*graphPort)))
	}
	for _, s := range []struct{ name, help string }{{"start", "Собрать и запустить среду, применить маунты"}, {"stop", "Остановить среду с сохранением данных"}, {"status", "Состояние среды"}, {"logs", "Логи сервера Orca"}, {"map", "Обновить карту проектов и знаний"}} {
		leaf(workspace, s.name, s.help, cobra.NoArgs, "workspace", s.name)
	}
	leaf(workspace, "shell [project]", "Открыть терминал внутри среды", cobra.MaximumNArgs(1), "workspace", "shell")
	project := &cobra.Command{Use: "project", Short: "Подключённые репозитории и источники знаний", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error { return c.Help() }}
	root.AddCommand(project)
	add := leaf(project, "add <path>", "Подключить существующий Git checkout", cobra.ExactArgs(1), "project", "add")
	id := add.Flags().String("id", "", "Стабильный идентификатор проекта")
	caps := add.Flags().StringSlice("capability", nil, "Возможности из delivery/catalog.json (через запятую)")
	ro := add.Flags().Bool("readonly", false, "Подключить для чтения")
	desc := add.Flags().String("description", "", "Назначение проекта для карты знаний")
	add.RunE = func(c *cobra.Command, args []string) error {
		v := append([]string{"project", "add"}, args...)
		if *id != "" {
			v = append(v, "--id", *id)
		}
		if *ro {
			v = append(v, "--readonly")
		}
		if *desc != "" {
			v = append(v, "--description", *desc)
		}
		for _, s := range *caps {
			v = append(v, "--capability", s)
		}
		return dispatch(v)
	}
	_ = add.RegisterFlagCompletionFunc("capability", func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		a, err := shared.New()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		catalog, err := a.DeliveryCatalog()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var result []string
		for name, capability := range catalog.Capabilities {
			if strings.HasPrefix(name, prefix) {
				result = append(result, name+"\t"+capability.Description)
			}
		}
		sort.Strings(result)
		return result, cobra.ShellCompDirectiveNoFileComp
	})
	leaf(project, "list", "Показать реестр проектов", cobra.NoArgs, "project", "list")
	leaf(project, "rm <id>", "Отключить проект, сохранив его файлы", cobra.ExactArgs(1), "project", "rm")
	leaf(project, "knowledge <id|global> <path> <origin>", "Зарегистрировать источник знаний", cobra.ExactArgs(3), "project", "knowledge")
	leaf(project, "relate <id> <other-id>", "Добавить связь между проектами", cobra.ExactArgs(2), "project", "relate")
	sync := leaf(root, "sync [project|path]", "Обновить инструкции, skills и MCP", cobra.MaximumNArgs(1), "sync")
	all := sync.Flags().Bool("all", false, "Синхронизировать все подключённые проекты")
	sync.RunE = func(c *cobra.Command, args []string) error {
		if *all {
			if len(args) > 0 {
				return errors.New("--all нельзя сочетать с отдельным проектом")
			}
			args = []string{"--all"}
		}
		return dispatch(append([]string{"sync"}, args...))
	}
	wt := &cobra.Command{Use: "worktree", Short: "Изолированные рабочие ветки с доставкой контекста", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error { return c.Help() }}
	root.AddCommand(wt)
	leaf(wt, "create <project> <name>", "Создать worktree и выполнить setup", cobra.ExactArgs(2), "worktree", "create")
	for _, s := range []struct{ name, help string }{{"orca", "Передать команду Orca внутри среды"}, {"task", "Передать команду orchestration task в Orca"}} {
		c := leaf(root, s.name+" [arguments...]", s.help, cobra.ArbitraryArgs, s.name)
		c.DisableFlagParsing = true
	}
	mcpGraph := leaf(root, "graphify-mcp <checkout>", "MCP Graphify для checkout", cobra.ExactArgs(1), "graphify-mcp")
	mcpGraph.Hidden = true
	runtimeCommands(root)
	helperCommands(root)
	migrate := leaf(root, "migrate <project> <old-agent-home>", "Импорт авторизаций или переход со старых devcontainer", cobra.ExactArgs(2), "migrate")
	legacyMigrationCommands(migrate)
	leaf(root, "restore <backup-directory>", "Восстановить backup выбранных авторизаций", cobra.ExactArgs(1), "restore")
	for _, s := range []struct{ name, help string }{{"ensure-image", "Собрать общий runtime"}, {"update", "Обновить платформу и образ"}, {"doctor", "Проверить установку и workspace"}} {
		leaf(root, s.name, s.help, cobra.NoArgs, s.name)
	}
	internal := leaf(root, "runtime-init", "Подготовить runtime перед Orca", cobra.NoArgs, "runtime-init")
	internal.Hidden = true
	serve := leaf(root, "runtime-serve", "Запустить сервер общей среды", cobra.NoArgs, "runtime-serve")
	serve.Hidden = true
	for _, item := range []struct {
		use   string
		count int
	}{{"runtime-legacy-archive <source>", 1}, {"runtime-legacy-import <backup> <account>", 2}, {"runtime-legacy-finish <fingerprint> <account>", 2}} {
		command := leaf(root, item.use, "Внутренний помощник миграции", cobra.ExactArgs(item.count), strings.Fields(item.use)[0])
		command.Hidden = true
	}
	leaf(root, "banner", "Показать подсказки среды и текущего проекта", cobra.NoArgs, "banner")
	omni := leaf(root, "omniroute [arguments...]", "CLI OmniRoute внутри общей среды", cobra.ArbitraryArgs, "omniroute")
	omni.DisableFlagParsing = true
	agent := leaf(root, "agent <name> [arguments...]", "Запустить агента после доставки контекста checkout", cobra.MinimumNArgs(1), "agent")
	agent.DisableFlagParsing = true
	return root
}
