package telemetry

// Langfuse links a generation to a managed prompt version through these
// attributes (Langfuse v4.72.0 attributes.ts). The link is made only on an
// observation of type generation, and the version must be an integer.
const (
	AttrObsPromptName    = "langfuse.observation.prompt.name"
	AttrObsPromptVersion = "langfuse.observation.prompt.version"
)
