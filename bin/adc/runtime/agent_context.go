package runtime

import (
	"path/filepath"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// Layouts describe each agent's native entry points. Context delivery only
// applies these declarations; the shared platform documents remain agent-neutral.
type agentContextLayout struct {
	HomeDirectories       []string
	WorkspaceInstructions []agentInstruction
	HomeInstructions      []agentInstruction
	ProjectInstructions   []agentInstruction
	Skills                []agentSkillDestination
	IgnorePatterns        []string
	InstallerPaths        []string
	AuthFiles             []string
}

type agentInstruction struct {
	Path, Body string
}

type agentSkillDestination struct {
	Path           string
	Common, Scoped bool
}

const workspaceAgentInstruction = "Прочитай /workspace/AGENTS.md и /workspace/AGENTS.workspace.md. Для изменений в проекте прочитай его AGENTS.md и AGENTS.platform.md. Проектные скиллы: его AGENTS.skills.md; выбирай форки только в их области. MCP с префиксом project/checkоut относится к указанному checkout.\n"

func (claudeDriver) Context() agentContextLayout {
	return agentContextLayout{
		HomeDirectories:       []string{".claude"},
		WorkspaceInstructions: []agentInstruction{{"CLAUDE.md", "@AGENTS.md\n@AGENTS.workspace.md\n"}},
		HomeInstructions:      []agentInstruction{{".claude/CLAUDE.md", "@/workspace/AGENTS.md\n@/workspace/AGENTS.workspace.md\n"}},
		ProjectInstructions:   []agentInstruction{{"CLAUDE.md", "@AGENTS.platform.md\n@/workspace/AGENTS.workspace.md\n@AGENTS.md"}},
		IgnorePatterns:        []string{"/.mcp.json"},
		AuthFiles:             []string{".claude.json", ".claude/.credentials.json"},
	}
}

func (codexDriver) Context() agentContextLayout {
	return agentContextLayout{
		HomeDirectories:  []string{".codex"},
		HomeInstructions: []agentInstruction{{".codex/AGENTS.md", workspaceAgentInstruction}},
		Skills:           []agentSkillDestination{{Path: ".codex/skills", Common: true, Scoped: true}},
		IgnorePatterns:   []string{"/.codex/config.toml"},
		AuthFiles:        []string{".codex/auth.json"},
	}
}

func (openCodeDriver) Context() agentContextLayout {
	return agentContextLayout{
		Skills:         []agentSkillDestination{{Path: ".config/opencode/skills", Scoped: true}},
		InstallerPaths: []string{".opencode/bin"},
	}
}

func (hermesDriver) Context() agentContextLayout {
	return agentContextLayout{
		HomeDirectories:  []string{".hermes"},
		HomeInstructions: []agentInstruction{{".hermes/AGENTS.md", workspaceAgentInstruction}},
		Skills:           []agentSkillDestination{{Path: ".hermes/skills", Scoped: true}},
		AuthFiles:        []string{".hermes/auth.json", ".hermes/.env"},
	}
}

func (dshDriver) Context() agentContextLayout {
	return agentContextLayout{
		HomeDirectories: []string{".dsh"},
		IgnorePatterns:  []string{"/.dsh/skills/"},
		AuthFiles:       []string{".dsh/.env"},
	}
}

var authFiles = func() []string {
	var paths []string
	for _, driver := range agentDrivers() {
		paths = append(paths, driver.Context().AuthFiles...)
	}
	return paths
}()

// This is the existing composed-skill storage location, shared by all drivers.
// Keep it stable independently of which agent adapters are registered.
func composedSkillsPath(root string) string {
	return filepath.Join(root, ".claude", "skills")
}

func writeWorkspaceAgentContext(workspace, home string, drivers []agentDriver) error {
	for _, driver := range drivers {
		layout := driver.Context()
		for _, instruction := range layout.WorkspaceInstructions {
			if err := shared.AtomicWrite(filepath.Join(workspace, instruction.Path), []byte(shared.Marker+instruction.Body), 0644); err != nil {
				return err
			}
		}
		for _, instruction := range layout.HomeInstructions {
			if err := shared.ManagedBlock(filepath.Join(home, instruction.Path), "<!-- adc:workspace:start -->", "<!-- adc:workspace:end -->", instruction.Body); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeProjectAgentContext(root string, drivers []agentDriver) error {
	instructions := []agentInstruction{{"AGENTS.md", "Прочитай [контракт ADC](AGENTS.platform.md) и [все проекты](/workspace/AGENTS.workspace.md). Правила этого файла и вложенных AGENTS.md сохраняют область действия."}}
	for _, driver := range drivers {
		instructions = append(instructions, driver.Context().ProjectInstructions...)
	}
	for _, instruction := range instructions {
		if err := shared.ManagedBlock(filepath.Join(root, instruction.Path), "<!-- adc:context:start -->", "<!-- adc:context:end -->", instruction.Body); err != nil {
			return err
		}
	}
	return nil
}
