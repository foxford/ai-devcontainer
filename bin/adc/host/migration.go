package host

import (
	"errors"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"strconv"
)

func (a *App) Migrate(args []string) error {
	if len(args) != 2 {
		return errors.New("adc migrate <project> <old-agent-home>")
	}
	if e := a.shared().RequireHost(); e != nil {
		return e
	}
	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	p, e := a.shared().Project(c, args[0])
	if e != nil {
		return e
	}
	old, e := shared.Abs(args[1])
	if e != nil {
		return e
	}
	v, e := mount(old, "/migration-source", true)
	if e != nil {
		return e
	}
	uid, gid := os.Getuid(), os.Getgid()
	if uid <= 0 {
		uid, gid = 1000, 1000
	}
	// A one-off container mounts only the explicitly selected account directory.
	return shared.Run("", "docker", "run", "--rm", "--user", strconv.Itoa(uid)+":"+strconv.Itoa(gid), "-e", "HOME=/home/node", "--mount", v, "--mount", "type=volume,src=adc-"+c.Name+"-home,dst=/home/node", "--mount", "type=bind,src="+a.State+",dst=/var/lib/adc", "--mount", "type=bind,src="+a.Platform+",dst=/opt/ai-devcontainer,readonly", "-e", "ADC_RUNTIME=1", "-e", "ADC_HOME=/var/lib/adc", "-e", "AI_DEVCONTAINER_HOME=/opt/ai-devcontainer", "--entrypoint", "adc", "adc-runtime:local", "migrate", p.ID, "/migration-source")
}
