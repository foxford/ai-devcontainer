package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

const StartupPath = "/tmp/adc-startup.json"

type StartupStatus struct {
	State   string    `json:"state"`
	Stage   string    `json:"stage"`
	Updated time.Time `json:"updated"`
}

var startupReport struct {
	sync.Mutex
	file string
}

// ReportStartup enables reporting only in this process. Environment variables
// would also enable it in Orca's terminals, agents and their ordinary syncs.
func ReportStartup(file string) func() {
	startupReport.Lock()
	previous := startupReport.file
	startupReport.file = file
	startupReport.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			startupReport.Lock()
			startupReport.file = previous
			startupReport.Unlock()
		})
	}
}

func StartupProgress(stage string) bool { return startupStatus("preparing", stage) }
func StartupFinished(err error) {
	if err != nil {
		startupStatus("failed", err.Error())
	} else {
		startupStatus("ready", "Среда готова")
	}
}

func startupStatus(state, stage string) bool {
	startupReport.Lock()
	defer startupReport.Unlock()
	if startupReport.file == "" {
		return false
	}
	fmt.Println("[adc]", stage)
	data, err := json.Marshal(StartupStatus{State: state, Stage: stage, Updated: time.Now().UTC()})
	if err == nil {
		err = AtomicWrite(startupReport.file, append(data, '\n'), 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "[adc] Не удалось записать этап запуска:", err)
	}
	return true
}
