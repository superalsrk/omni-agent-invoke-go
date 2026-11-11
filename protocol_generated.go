// Generated identity contract v2. SHA-256: dd65e267834a6aca67410a4b268f915b485c9355c3a027225391f06c31012adc. MIT License.
package omni

type Mode string

const (
	Canonical Mode = "canonical"
	Native    Mode = "native"
)

type RunRef struct {
	RunID             string `json:"run_id"`
	InvocationID      string `json:"invocation_id"`
	AgentSessionID    string `json:"agent_session_id"`
	SessionGeneration int64  `json:"session_generation"`
	ProtocolMode      Mode   `json:"protocol_mode"`
	KernelInstanceID  string `json:"kernel_instance_id"`
	StreamID          string `json:"stream_id"`
	Protocol          string `json:"protocol"`
}

const (
	MethodStatus        = "status"
	MethodList          = "adapter.list"
	MethodInvoke        = "agent_run.invoke"
	MethodGet           = "agent_run.get"
	MethodSubscribe     = "agent_run.subscribe"
	MethodCancel        = "agent_run.cancel"
	MethodRespond       = "agent_run.respond"
	MethodRespondNative = "agent_run.native.respond"
)
