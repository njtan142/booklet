package pdf

type PageInfo struct {
	PageNumber int
	Text       string
	Width      float64
	Height     float64
	LocalPath  string
}

type BookletConfig struct {
	Margin        float64 // Margin in PDF points (1/72 inch)
	Gutter        float64 // Gutter spacing between pages in PDF points
	PaperSize     string  // "A4" or "Letter"
	SignatureSize int     // e.g. 4, 8, 16
	Guides        bool    // Draw folding/cutting guides
}

type DBPageInfo struct {
	PageNumber  int
	StoragePath string
	Width       float64
	Height      float64
}

type SheetSide struct {
	LeftPage  int // 1-based page number, 0 for blank
	RightPage int // 1-based page number, 0 for blank
}
