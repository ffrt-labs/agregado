package digestartifact

// PROTOTYPE (summary bake-off, agregado#86) — lives only on the
// prototype/summary-bakeoff branch. Exposes the real candidate preparation so
// the harness feeds the selection call exactly what production would.
func PrototypePrepare(articles []Article, floor, max int) []Candidate {
	return uniqueAndDiverse(articles, floor, max).items
}
