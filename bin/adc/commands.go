package main

import (
	"errors"
	"github.com/foxford/ai-devcontainer/bin/adc/host"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/spf13/cobra"
	"os"
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
	root.Version = shared.BuildVersion()
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
	replaceLegacy := leaf(root, "bootstrap-replace-legacy <path>", "Сохранить старую платформу и переключить её на main", cobra.ExactArgs(1), "bootstrap-replace-legacy")
	replaceLegacy.Hidden = true
	applyUpdate := &cobra.Command{Use: "bootstrap-apply-update", Hidden: true, Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error {
		a, err := shared.New()
		if err != nil {
			return err
		}
		if err = a.RequireHost(); err != nil {
			return err
		}
		h := (*host.App)(a)
		return h.ApplyUpdate(func() error { return h.Install(false, false, root) })
	}}
	root.AddCommand(applyUpdate)
	workspace := &cobra.Command{Use: "workspace", Short: "Управление общей Docker средой", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error { return c.Help() }}
	root.AddCommand(workspace)
	init := leaf(workspace, "init [name]", "Создать workspace без изменения проектов", cobra.MaximumNArgs(1), "workspace", "init")
	port := init.Flags().Int("port", shared.DefaultOrcaPort, "Порт Orca на 127.0.0.1")
	omniPort := init.Flags().Int("omniroute-port", 20128, "Порт OmniRoute на 127.0.0.1")
	graphPort := init.Flags().Int("graph-port", 4242, "Порт просмотра графа на 127.0.0.1")
	init.RunE = func(c *cobra.Command, args []string) error {
		return dispatch(append(append([]string{"workspace", "init"}, args...), "--port", strconv.Itoa(*port), "--omniroute-port", strconv.Itoa(*omniPort), "--graph-port", strconv.Itoa(*graphPort)))
	}
	config := leaf(workspace, "config", "Показать или изменить порты и USB workspace", cobra.NoArgs, "workspace", "config")
	config.Long = "Показать сохранённые порты и USB или изменить явно переданные значения. Повторяемый --publish HOST:CONTAINER или IP:HOST:CONTAINER заменяет весь список пользовательских TCP-портов; --publish-clear очищает его. По умолчанию порты доступны на 127.0.0.1; IPv6-адрес указывается в квадратных скобках. Сервис внутри контейнера должен слушать 0.0.0.0 или подходящий IPv6-адрес. Повторяемый --usb VID:PID заменяет весь список USB; --usb-clear очищает его. Выбор USB сохраняется и без подключённых устройств. Применить изменения: adc workspace start. Изменение параметров запуска пересоздаст контейнер и завершит активные сеансы."
	config.Flags().Int("port", 0, "Новый порт Orca на 127.0.0.1")
	config.Flags().Int("omniroute-port", 0, "Новый порт OmniRoute на 127.0.0.1")
	config.Flags().Int("graph-port", 0, "Новый порт просмотра графа на 127.0.0.1")
	publishedPorts := config.Flags().StringArray("publish", nil, "TCP HOST:CONTAINER или IP:HOST:CONTAINER, например 3000:3000; повторите для нескольких портов")
	config.Flags().Bool("publish-clear", false, "Очистить список пользовательских портов")
	config.MarkFlagsMutuallyExclusive("publish", "publish-clear")
	_ = config.RegisterFlagCompletionFunc("publish", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
	usbDevices := config.Flags().StringArray("usb", nil, "USB VID:PID, например 2104:0313; повторите для нескольких устройств (Linux)")
	config.Flags().Bool("usb-clear", false, "Очистить список USB-устройств")
	config.MarkFlagsMutuallyExclusive("usb", "usb-clear")
	_ = config.RegisterFlagCompletionFunc("usb", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})
	config.RunE = func(c *cobra.Command, args []string) error {
		argv := []string{}
		for _, name := range []string{"port", "omniroute-port", "graph-port"} {
			if c.Flags().Changed(name) {
				value, _ := c.Flags().GetInt(name)
				argv = append(argv, "--"+name, strconv.Itoa(value))
			}
		}
		if c.Flags().Changed("publish") {
			for _, value := range *publishedPorts {
				argv = append(argv, "--publish", value)
			}
		}
		if c.Flags().Changed("publish-clear") {
			value, _ := c.Flags().GetBool("publish-clear")
			argv = append(argv, "--publish-clear="+strconv.FormatBool(value))
		}
		if c.Flags().Changed("usb") {
			for _, value := range *usbDevices {
				argv = append(argv, "--usb", value)
			}
		}
		if c.Flags().Changed("usb-clear") {
			value, _ := c.Flags().GetBool("usb-clear")
			argv = append(argv, "--usb-clear="+strconv.FormatBool(value))
		}
		a, err := shared.New()
		if err != nil {
			return err
		}
		return (*host.App)(a).WorkspaceConfig(argv, c.OutOrStdout())
	}
	for _, s := range []struct{ name, help string }{{"start", "Запустить среду в фоне и дождаться готовности с выводом этапов"}, {"stop", "Остановить среду с сохранением данных"}, {"status", "Состояние контейнера и готовность среды"}, {"map", "Обновить карту проектов и знаний"}} {
		leaf(workspace, s.name, s.help, cobra.NoArgs, "workspace", s.name)
	}
	logs := leaf(workspace, "logs", "Полные логи подготовки среды и сервера Orca", cobra.NoArgs, "workspace", "logs")
	tail := logs.Flags().String("tail", "all", "Число последних строк или all для полного журнала")
	follow := logs.Flags().BoolP("follow", "f", false, "Продолжать вывод новых строк")
	logs.RunE = func(c *cobra.Command, args []string) error {
		argv := []string{"workspace", "logs", "--tail", *tail}
		if *follow {
			argv = append(argv, "--follow")
		}
		return dispatch(argv)
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
	capability := &cobra.Command{Use: "capability", Short: "Каталог и возможности текущего проекта", Args: cobra.NoArgs, RunE: func(c *cobra.Command, args []string) error {
		return dispatch([]string{"project", "capability", "list"})
	}}
	project.AddCommand(capability)
	leaf(capability, "list", "Показать каталог; ✓ отмечает выбранные возможности", cobra.NoArgs, "project", "capability", "list")
	for _, action := range []string{"add", "rm"} {
		help := "Добавить возможности текущему проекту и синхронизировать доставку"
		if action == "rm" {
			help = "Удалить командные возможности текущего проекта и синхронизировать доставку"
		}
		command := leaf(capability, action+" <имя> [имя...]", help, cobra.MinimumNArgs(1), "project", "capability", action)
		command.ValidArgsFunction = func(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
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
				if strings.HasPrefix(name, prefix) && !shared.Contains(args, name) {
					result = append(result, name+"\t"+capability.Description)
				}
			}
			sort.Strings(result)
			return result, cobra.ShellCompDirectiveNoFileComp
		}
	}
	leaf(project, "rm <id>", "Отключить проект, сохранив его файлы", cobra.ExactArgs(1), "project", "rm")
	leaf(project, "knowledge <id|global> <path> <origin>", "Зарегистрировать источник знаний", cobra.ExactArgs(3), "project", "knowledge")
	leaf(project, "relate <id> <other-id>", "Добавить связь между проектами", cobra.ExactArgs(2), "project", "relate")
	sync := leaf(root, "sync [project|path]", "Обновить инструкции, skills и MCP", cobra.MaximumNArgs(1), "sync")
	sync.Hidden = true // Existing setup hooks still invoke this internal delivery step.
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
	refreshGraph := leaf(root, "graphify-refresh <checkout>", "Фоновое обновление индекса checkout", cobra.ExactArgs(1), "graphify-refresh")
	refreshGraph.Hidden = true
	runtimeCommands(root)
	helperCommands(root)
	migrate := leaf(root, "migrate <project> <old-agent-home>", "Импорт авторизаций или переход со старых devcontainer", cobra.ExactArgs(2), "migrate")
	legacyMigrationCommands(migrate)
	leaf(root, "restore <backup-directory>", "Восстановить backup выбранных авторизаций", cobra.ExactArgs(1), "restore")
	for _, s := range []struct{ name, help string }{{"ensure-image", "Собрать общий runtime"}, {"update", "Обновить ADC и перезапустить работающий workspace"}, {"doctor", "Проверить установку и workspace"}} {
		command := leaf(root, s.name, s.help, cobra.NoArgs, s.name)
		if s.name == "doctor" {
			check := &cobra.Command{Use: "project <path|id>", Short: "Проверить проект, состояние доставки и ссылки без изменений", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
				a, err := shared.New()
				if err != nil {
					return err
				}
				return (*host.App)(a).DoctorProject(args[0], cmd.OutOrStdout())
			}}
			command.AddCommand(check)
		}
	}
	updateCommands(root)
	internal := leaf(root, "runtime-init", "Подготовить runtime перед Orca", cobra.NoArgs, "runtime-init")
	internal.Hidden = true
	serve := leaf(root, "runtime-serve", "Запустить сервер общей среды", cobra.NoArgs, "runtime-serve")
	serve.Hidden = true
	for _, item := range []struct {
		use   string
		count int
	}{{"runtime-legacy-archive <source>", 1}, {"runtime-legacy-shared-archive <source>", 1}, {"runtime-legacy-import <backup> <account>", 2}, {"runtime-legacy-finish <fingerprint> <account>", 2}} {
		command := leaf(root, item.use, "Внутренний помощник миграции", cobra.ExactArgs(item.count), strings.Fields(item.use)[0])
		command.Hidden = true
	}
	leaf(root, "banner", "Показать подсказки среды и текущего проекта", cobra.NoArgs, "banner")
	omni := leaf(root, "omniroute [arguments...]", "CLI OmniRoute внутри общей среды", cobra.ArbitraryArgs, "omniroute")
	omni.DisableFlagParsing = true
	agent := leaf(root, "agent <name> [arguments...]", "Запустить агента после доставки контекста checkout", cobra.MinimumNArgs(1), "agent")
	agent.DisableFlagParsing = true
	configureCommandSide(root, os.Getenv("ADC_RUNTIME") == "1")
	completionCommands(root)
	return root
}

// Remove unavailable commands from help, completion and command resolution.
// Commands forwarded by the host to the runtime remain available on both sides.
func configureCommandSide(root *cobra.Command, runtime bool) {
	remove := func(parent *cobra.Command, names ...string) {
		for _, command := range parent.Commands() {
			if shared.Contains(names, command.Name()) {
				parent.RemoveCommand(command)
			}
		}
	}
	if !runtime {
		root.Short = "Общая изолированная среда разработчика — управление на хосте"
		remove(root, "banner", "runtime-init", "runtime-serve", "runtime-legacy-archive", "runtime-legacy-shared-archive", "runtime-legacy-import", "runtime-legacy-finish", "graphify-mcp", "graphify-refresh")
		return
	}
	root.Short = "Общая среда разработчика — команды внутри контейнера"
	remove(root, "install", "bootstrap-replace-legacy", "bootstrap-apply-update", "ensure-image")
	for _, command := range root.Commands() {
		if command.Name() == "project" {
			for _, child := range command.Commands() {
				if child.Name() != "list" && child.Name() != "capability" {
					command.RemoveCommand(child)
				}
			}
			command.Short = "Подключённые репозитории"
		}
		if command.Name() == "workspace" {
			for _, child := range command.Commands() {
				if child.Name() != "map" {
					command.RemoveCommand(child)
				}
			}
			command.Short = "Карта подключённых проектов"
		}
		if command.Name() == "migrate" {
			remove(command, "legacy")
			command.Short = "Импорт авторизаций агента"
		}
	}
}
