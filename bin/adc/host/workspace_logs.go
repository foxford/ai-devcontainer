package host

import (
	"errors"
	"flag"
	"io"
	"strconv"
)

func runWorkspaceLogs(container string, args []string, run func(string, string, ...string) error) error {
	flags := flag.NewFlagSet("workspace logs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	tail := flags.String("tail", "all", "")
	follow := flags.Bool("follow", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("adc workspace logs не принимает позиционные аргументы")
	}
	if *tail != "all" {
		if _, err := strconv.ParseUint(*tail, 10, 64); err != nil {
			return errors.New("--tail: укажите неотрицательное число строк или all")
		}
	}
	argv := []string{"logs", "--tail", *tail}
	if *follow {
		argv = append(argv, "--follow")
	}
	return run("", "docker", append(argv, container)...)
}
