package fixtures

// tool_factory.go is a DOCUMENTED STUB for Wave 1.
//
// The ToolDefinition message does NOT exist in the experience.v1 contract yet
// — it lands with TRD 140-07 (typed tool/adapter contract). Per this TRD's
// scope discipline ("keep factories MINIMAL for Wave 1; grow them per-
// message-group in each downstream TRD's Task 1"), we deliberately do NOT
// reference a non-existent type here. Doing so would not compile.
//
// When 140-07 generates experiencev1.ToolDefinition, its Task 1 fleshes this
// file out with the functional-options builder below:
//
//	type ToolOpt func(*experiencev1.ToolDefinition)
//
//	// NewTool returns a valid ToolDefinition with a default adapter_id drawn
//	// from the allowlist and a typed input/output schema, then applies opts.
//	func NewTool(opts ...ToolOpt) *experiencev1.ToolDefinition {
//	    tool := &experiencev1.ToolDefinition{
//	        // adapter_id: first entry of the 140-07 adapter allowlist
//	        // input_schema / output_schema: typed defaults
//	    }
//	    for _, opt := range opts {
//	        opt(tool)
//	    }
//	    return tool
//	}
//
//	func WithAdapter(id string) ToolOpt { ... }
//
// Until then this file intentionally carries no executable code so the
// package compiles against today's generated types only.
