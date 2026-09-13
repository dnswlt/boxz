// Package boxz parses manually structured graph descriptions and renders them
// as deterministic orthogonal SVG diagrams.
//
// Container order fixes the coarse layout. The renderer may enlarge
// spring-enabled nodes, spring gaps, seams, and outer channels to use available
// space or fit edge lanes, but it never reorders or otherwise optimizes the
// user's node placement.
//
// Parse returns an opaque, valid Document. Nodes and Edges return copied views;
// WithEdges creates another validated document without mutating its receiver.
// RenderSVG consumes that model directly, while Format writes its canonical
// source representation for persistence.
package boxz
