package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const StartupPath = "/tmp/adc-startup.json"

type StartupStatus struct {
	State   string    `json:"state"`
	Stage   string    `json:"stage"`
	Updated time.Time `json:"updated"`
}

func StartupProgress(stage string) { startupStatus("preparing", stage) }
func StartupFinished(err error) {
	if err != nil {
		startupStatus("failed", err.Error())
	} else {
		startupStatus("ready", "Среда готова")
	}
}

func startupStatus(state, stage string) {
	file := os.Getenv("ADC_STARTUP_STATUS")
	if file == "" {
		return
	}
	fmt.Println("[adc]", stage)
	data, err := json.Marshal(StartupStatus{State: state, Stage: stage, Updated: time.Now().UTC()})
	if err == nil {
		err = AtomicWrite(file, append(data, '\n'), 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "[adc] Не удалось записать этап запуска:", err)
	}
}
