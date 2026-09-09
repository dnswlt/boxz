// Package boxz parses manually structured graph descriptions and renders them
// as deterministic orthogonal SVG diagrams.
//
// Container order fixes the coarse layout. The renderer may enlarge nodes,
// seams, and outer channels to fit ports and edge lanes, but it never reorders
// or otherwise optimizes the user's node placement.
package boxz
