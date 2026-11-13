package harness

import (
	"github.com/giocaizzi/agents/internal/config"
)

// The shared enums autonomy.execution_mode and autonomy.sandbox_mode map to
// each harness's native values below. A value with no exact native equivalent
// is never approximated: nothing is written for that harness and the render
// warns. The same tables are documented as evidence in internal/feature.

// Shared keys, as the warnings name them, and the native name that is auto.
const (
	nativeAuto    = "auto"
	nativeDefault = "default"
	keyExecution  = "autonomy.execution_mode"
	keySandbox    = "autonomy.sandbox_mode"
)

// nativeMode looks the value of a shared key up in table. An empty value is
// "not set"; a value missing from the table has no native equivalent, so it
// warns, naming the harness and its native key, and returns "".
func (b *builder) nativeMode(shared, value, harness, key string, table map[string]string) string {
	if value == "" {
		return ""
	}
	if v, ok := table[value]; ok {
		return v
	}
	b.warnf("%s = %q: %s has no exact equivalent for it in %s, so nothing is written", shared, value, harness, key)
	return ""
}

// sandboxEnabled is the {enabled: true} object of a harness whose sandbox is
// a nested boolean, or nil when the shared mode does not turn it on.
func (b *builder) sandboxEnabled(value, harness, key string) map[string]any {
	if b.sandboxOn(value, harness, key) {
		return map[string]any{keyEnabled: true}
	}
	return nil
}

// sandboxOn maps the shared sandbox mode to an on/off native setting:
// workspace is on, off is off, and read-only has no equivalent (warned, so
// off here too).
func (b *builder) sandboxOn(value, harness, key string) bool {
	table := map[string]string{config.SandboxWorkspace: "on", config.SandboxOff: "off"}
	return b.nativeMode(keySandbox, value, harness, key, table) == "on"
}

// Execution mode tables: ask, auto, full to the native value. Absent = none.

func claudeExecution() map[string]string {
	return map[string]string{config.ExecutionAsk: nativeDefault, config.ExecutionAuto: nativeAuto, config.ExecutionFull: "bypassPermissions"}
}

func copilotExecution() map[string]string {
	return map[string]string{config.ExecutionAsk: "manual", config.ExecutionAuto: "assisted", config.ExecutionFull: "allow-all"}
}

func vscodeExecution() map[string]string {
	return map[string]string{config.ExecutionAsk: nativeDefault, config.ExecutionFull: "autoApprove"}
}

func codexExecution() map[string]string {
	return map[string]string{config.ExecutionAuto: "on-request", config.ExecutionFull: "never"}
}

func codexSandbox() map[string]string {
	return map[string]string{config.SandboxOff: "danger-full-access", config.SandboxWorkspace: "workspace-write", config.SandboxReadOnly: "read-only"}
}

// antigravityExecution is agentMode. full also sets toolPermission to
// always-proceed, since agentMode alone still pauses risky commands.
func antigravityExecution() map[string]string {
	return map[string]string{config.ExecutionAsk: nativeDefault, config.ExecutionAuto: nativeAuto, config.ExecutionFull: "auto"}
}
