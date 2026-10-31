// Generated identity contract v1. SHA-256: e37f9facc6af9737602a06c31801c76efbd39ffa4fe6f96ac220d0c841c35176. MIT License.
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
