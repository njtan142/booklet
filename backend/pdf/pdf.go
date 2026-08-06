package pdf

// pdf.go is intentionally kept minimal.
// Its implementation has been refactored and split into:
// - types.go (Data structures and configurations)
// - split.go (PDF splitting and parsing logic)
// - layout.go (Booklet imposition layout math)
// - render.go (PDF merging, image fallbacks, optimization)
// - compile.go (Booklet compilation and slicing)
