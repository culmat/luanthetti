// Deviations from luanti-depgraph (ldg), documented so the conformance tests can account
// for them:
//
//  1. Local directories get no static analysis. ldg runs its tree-sitter analyzer on a local
//     target to find undeclared references; this port has none, so a local target is never
//     "risky" and CompatReport sets AnalyzedSources=false for it (ldg sets true).
//  2. Index.Game does not retry "minetest" as "minetest_game": both normalise to the same
//     string, so the loop already covers it, and ldg's retry recurses when Minetest Game is
//     absent from the snapshot.
//  3. Empty lists marshal as [] (ldg's JSON has [] too; Go would otherwise emit null).
//  4. Summary.Score rounds half away from zero; Python rounds half to even on the binary
//     value. The golden tests compare scores with a tolerance of 0.051.
//  5. rdeps, blockers, root_causes, lint, sql and cypher are not ported; Luanthetti does not
//     need them.
package depgraph
