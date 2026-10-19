package ai

// usage is the token count Workers AI reports for one call.
type usage struct{ Prompt, Completion int }

// neuronRates are Neurons per million input/output tokens
// (developers.cloudflare.com/workers-ai/platform/pricing), for the models
// Agregado runs. Used only to log an estimate against the 10k/day free
// allowance (ADR 0009 measured ~520/day for the structured Summary).
var neuronRates = map[string][2]float64{
	"@cf/google/gemma-4-26b-a4b-it": {9091, 27273},
	"@cf/openai/gpt-oss-120b":       {31818, 68182},
}

// neurons estimates the cost of u on model; ok is false for an unpriced model.
func (u usage) neurons(model string) (n float64, ok bool) {
	r, ok := neuronRates[model]
	if !ok {
		return 0, false
	}
	return (float64(u.Prompt)*r[0] + float64(u.Completion)*r[1]) / 1e6, true
}
