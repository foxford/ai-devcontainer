package host

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// WorkspaceConfig reads or saves host settings without contacting Docker or USB
// hardware, including when the workspace is stopped or a device is disconnected.
func (a *App) WorkspaceConfig(args []string, out io.Writer) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	flags := flag.NewFlagSet("workspace config", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	port := flags.Int("port", 0, "")
	omniPort := flags.Int("omniroute-port", 0, "")
	graphPort := flags.Int("graph-port", 0, "")
	var usbDevices []string
	flags.Func("usb", "", func(value string) error {
		usbDevices = append(usbDevices, strings.ToLower(value))
		return nil
	})
	clearUSB := flags.Bool("usb-clear", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("adc workspace config не принимает позиционные аргументы")
	}
	changed := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { changed[f.Name] = true })
	if changed["usb"] && changed["usb-clear"] {
		return errors.New("--usb и --usb-clear нельзя использовать вместе")
	}
	var c shared.Config
	var err error
	if len(changed) == 0 {
		c, err = a.shared().Load()
	} else {
		err = a.shared().LockedFile("lifecycle.lockfile", func() error {
			return a.shared().Locked(func() error {
				// Reload after both locks so a concurrent project registration is
				// preserved, and refuse changes that would invalidate recovery.
				var err error
				c, err = a.shared().Load()
				if err != nil {
					return err
				}
				if _, err := os.Lstat(filepath.Join(a.State, "migration-pending.json")); !os.IsNotExist(err) {
					if err != nil {
						return err
					}
					return errors.New("миграция не завершена: adc migrate legacy finish; изменение настроек заблокировано")
				}
				if pending, err := a.readReplacement(c); err != nil {
					return err
				} else if pending != nil {
					return errors.New("переключение контейнера не завершено: adc workspace start; изменение настроек заблокировано")
				}
				for name, value := range map[string]*int{"port": port, "omniroute-port": omniPort, "graph-port": graphPort} {
					if changed[name] && (*value < 1 || *value > 65535) {
						return fmt.Errorf("--%s: порт должен быть числом от 1 до 65535", name)
					}
				}
				if changed["port"] {
					c.Port = *port
				}
				if changed["omniroute-port"] {
					c.OmniRoutePort = *omniPort
				}
				if changed["graph-port"] {
					c.GraphPort = *graphPort
				}
				if changed["usb"] {
					c.USBDevices = usbDevices
				} else if *clearUSB {
					c.USBDevices = nil
				}
				if err := c.ValidatePorts(); err != nil {
					return err
				}
				if err := c.ValidateUSBDevices(); err != nil {
					return err
				}
				return a.shared().Save(c)
			})
		})
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Workspace: %s\nСохранённые порты на хосте:\n  Orca: 127.0.0.1:%d (--port)\n  OmniRoute: 127.0.0.1:%d (--omniroute-port)\n  Граф: 127.0.0.1:%d (--graph-port)\n", c.Name, c.Port, c.GatewayPort(), c.PreviewPort())
	if len(c.USBDevices) == 0 {
		fmt.Fprintln(out, "USB: устройства не выбраны (--usb VID:PID).")
	} else {
		fmt.Fprintf(out, "Выбранные USB (VID:PID): %s\n", strings.Join(c.USBDevices, ", "))
	}
	if len(changed) > 0 {
		fmt.Fprintln(out, "Применить изменения: adc workspace start. Изменение параметров запуска пересоздаст контейнер и завершит активные сеансы.")
	}
	return nil
}
