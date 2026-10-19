package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const codexScopeStart = "# ai-devcontainer:mcp-scope:start"
const codexScopeEnd = "# ai-devcontainer:mcp-scope:end"

// Names delivered by ADC contain only ASCII letters, digits, '-' and '_'.
// Match their explicit tables, never arbitrary user keys or similarly named MCPs.
var codexServerTable = regexp.MustCompile(`^\[\s*(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\.\s*(?:"([A-Za-z0-9_-]+)"|'([A-Za-z0-9_-]+)'|([A-Za-z0-9_-]+))\s*(?:\.\s*([^\]]+))?\]\s*(?:#.*)?$`)
var codexEnabledKey = regexp.MustCompile(`^(?:enabled|"enabled"|'enabled')\s*=`)
var codexHeadersKey = regexp.MustCompile(`^(?:http_headers|"http_headers"|'http_headers')\s*(?:=|\.)`)
var codexInlineServer = regexp.MustCompile(`^(?:"([A-Za-z0-9_-]+)"|'([A-Za-z0-9_-]+)'|([A-Za-z0-9_-]+))\s*=\s*\{`)
var codexServersTable = regexp.MustCompile(`^\[\s*(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*\]\s*(?:#.*)?$`)
var codexInlineRoot = regexp.MustCompile(`^(?:mcp_servers|"mcp_servers"|'mcp_servers')\s*=\s*\{`)

func checkCodexScopeFormat(path string, data []byte) error {
	multiline := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if multiline == "" {
			if strings.HasPrefix(trimmed, "[") {
				break
			}
			if codexInlineRoot.MatchString(trimmed) {
				return fmt.Errorf("%s: для области MCP checkout разверните mcp_servers = {...} в таблицы [mcp_servers.<имя>]; исходная конфигурация сохранена", path)
			}
		}
		multiline = codexStringState(line, multiline)
	}
	return nil
}

// Inline tables are explicitly authored user entries. Extending them with a
// generated table would invalidate TOML, so leave their contents and scope alone.
func codexInlineServers(data string) map[string]bool {
	result := map[string]bool{}
	multiline, section := "", ""
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if multiline == "" {
			if strings.HasPrefix(trimmed, "[") {
				section = trimmed
			}
			candidate := ""
			if codexServersTable.MatchString(section) {
				candidate = trimmed
			} else if section == "" && strings.HasPrefix(trimmed, "mcp_servers.") {
				candidate = strings.TrimPrefix(trimmed, "mcp_servers.")
			}
			if match := codexInlineServer.FindStringSubmatch(candidate); len(match) > 0 {
				for _, name := range match[1:] {
					if name != "" {
						result[name] = true
					}
				}
			}
		}
		multiline = codexStringState(line, multiline)
	}
	return result
}

// Ignore table-looking text inside multiline TOML strings. Native Codex writes
// ordinary tables, but the surrounding user configuration can contain prompts.
func codexStringState(line, multiline string) string {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		if multiline != "" {
			if strings.HasPrefix(line[i:], multiline) {
				i += 2
				multiline = ""
			} else if multiline == `"""` && line[i] == '\\' {
				i++
			}
			continue
		}
		if quote != 0 {
			if quote == '"' && line[i] == '\\' {
				i++
			} else if line[i] == quote {
				quote = 0
			}
			continue
		}
		if line[i] == '#' {
			break
		}
		if strings.HasPrefix(line[i:], `"""`) || strings.HasPrefix(line[i:], "'''") {
			multiline = line[i : i+3]
			i += 2
		} else if line[i] == '"' || line[i] == '\'' {
			quote = line[i]
		}
	}
	return multiline
}

func codexTableName(line string) string {
	name, child := codexTableParts(line)
	if child != "" {
		return ""
	}
	return name
}

func codexTableParts(line string) (string, string) {
	match := codexServerTable.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) == 0 {
		return "", ""
	}
	for _, name := range match[1:4] {
		if name != "" {
			return name, strings.Trim(strings.TrimSpace(match[4]), `"'`)
		}
	}
	return "", ""
}

func disableCodexMCP(path string, managed map[string]bool) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var lines []string
	multiline := ""
	ours := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if multiline == "" && strings.HasPrefix(trimmed, "[") {
			ours = managed[codexTableName(trimmed)]
			lines = append(lines, line)
			if ours {
				lines = append(lines, "enabled = false")
			}
		} else if multiline != "" || !ours || !codexEnabledKey.MatchString(trimmed) {
			lines = append(lines, line)
		}
		multiline = codexStringState(line, multiline)
	}
	result := strings.Join(lines, "\n")
	if result == string(data) {
		return nil
	}
	return secureWrite(path, []byte(result))
}

func codexProjectConfig(path string, names []string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(names) > 0 {
		if err := checkCodexScopeFormat(path, data); err != nil {
			return nil, err
		}
	}
	human, err := composeBlock(string(data), codexScopeStart, codexScopeEnd, "")
	if err != nil {
		return nil, err
	}
	// A human's explicit project table takes precedence over the generated flag.
	existing := codexInlineServers(human)
	multiline := ""
	for _, line := range strings.Split(human, "\n") {
		if multiline == "" {
			existing[codexTableName(line)] = true
		}
		multiline = codexStringState(line, multiline)
	}
	var block strings.Builder
	for _, name := range names {
		if !existing[name] {
			quoted, _ := json.Marshal(name)
			fmt.Fprintf(&block, "[mcp_servers.%s]\nenabled = true\n", quoted)
		}
	}
	result, err := composeBlock(human, codexScopeStart, codexScopeEnd, block.String())
	return []byte(result), err
}

func scopeCodexMCP(d agentDelivery, projectPath string, managed map[string]bool) error {
	config := codexConfigPath(d.Home)
	if err := disableCodexMCP(config, managed); err != nil {
		return err
	}
	names := []string{}
	for _, name := range sortedKeys(d.Servers) {
		if managed[name] && codexEntryKind(config, name) != "none" {
			names = append(names, name)
		}
	}
	data, err := codexProjectConfig(projectPath, names)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		if _, err := os.Lstat(projectPath); os.IsNotExist(err) {
			return nil
		}
	}
	return secureWrite(projectPath, data)
}
