package main

import (
	"github.com/spf13/cobra"
	"strconv"
)

func helperCommands(root *cobra.Command) {
	tools := &cobra.Command{Use: "tools", Short: "Инструменты общей среды", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	root.AddCommand(tools)
	leaf := func(parent *cobra.Command, use, short string, n cobra.PositionalArgs, prefix ...string) *cobra.Command {
		cmd := &cobra.Command{Use: use, Short: short, Args: n, RunE: func(_ *cobra.Command, args []string) error {
			return dispatch(append(append([]string{"tools"}, prefix...), args...))
		}}
		parent.AddCommand(cmd)
		return cmd
	}
	installer := leaf(tools, "install [graphify|omniroute]", "Установить или восстановить инструменты общей среды", cobra.MaximumNArgs(1), "install")
	installer.ValidArgs = []string{"graphify", "omniroute"}
	snapshot := leaf(tools, "claude-snapshot", "Архивы переписки Claude Code в общем home", cobra.NoArgs, "claude-snapshot", "snapshot")
	for _, name := range []string{"daily", "list", "retention"} {
		leaf(snapshot, name, map[string]string{"daily": "Создать архив, если последний старше суток", "list": "Показать архивы", "retention": "Увеличить retention, сохранив явную настройку"}[name], cobra.NoArgs, "claude-snapshot", name)
	}
	leaf(snapshot, "restore <archive>", "Распаковать рядом с текущими сессиями", cobra.ExactArgs(1), "claude-snapshot", "restore")
	graph := &cobra.Command{Use: "graphify", Short: "Локальный индекс кода и просмотр графа проекта", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	tools.AddCommand(graph)
	leaf(graph, "index", "Создать или обновить индекс checkout без LLM", cobra.NoArgs, "graphify", "index")
	leaf(graph, "label-paths", "Назвать сообщества по путям без LLM и обновить HTML", cobra.NoArgs, "graphify", "label-paths")
	view := leaf(graph, "view", "Показать интерактивный граф в браузере", cobra.NoArgs, "graphify", "view")
	port := view.Flags().Int("port", 4242, "Порт HTTP-сервера внутри среды")
	view.RunE = func(_ *cobra.Command, _ []string) error {
		return dispatch([]string{"tools", "graphify", "view", strconv.Itoa(*port)})
	}
	patch := leaf(tools, "pnpm-patch-dep <package> [version|major]", "Обновить транзитивную зависимость в пределах её major", cobra.RangeArgs(1, 2), "pnpm-patch-dep")
	leaf(patch, "doctor", "Проверить overrides и зависимости, удерживающие старые версии", cobra.NoArgs, "pnpm-patch-dep", "doctor")
}
