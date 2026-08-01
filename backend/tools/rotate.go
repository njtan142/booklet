package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"booklet/jobs"
	"booklet/logger"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// rotateParams is the parameter set for the Rotate tool.
type rotateParams struct {
	// Angle is clockwise degrees. pdfcpu accepts any multiple of 90, including
	// negatives, but the UI offers the three that matter.
	Angle int `json:"angle"`
	// Pages is an optional pdfcpu page selection such as "1-4,7,9-". Empty
	// rotates the whole document.
	Pages string `json:"pages"`
}

func init() {
	Register(&Tool{
		Slug:        "rotate",
		Label:       "Rotate PDF",
		Description: "Rotate pages by 90, 180 or 270 degrees.",
		Icon:        "rotate-cw",
		MinInputs:   1,
		MaxInputs:   1,
		InputKinds:  []string{"pdf"},
		// Rotation moves glyphs on the page but changes none of them, so the
		// parent's embeddings are still exactly right. This is what keeps a
		// 500-page rotate from issuing 500 Ollama calls.
		PreservesText: true,
		Params: []Param{
			{
				Name:     "angle",
				Label:    "Rotation",
				Type:     ParamEnum,
				Required: true,
				Default:  "90",
				Options:  []string{"90", "180", "270"},
				Help:     "Clockwise rotation applied to the selected pages.",
			},
			{
				Name:  "pages",
				Label: "Pages",
				Type:  ParamPageRange,
				Help:  "Leave empty to rotate every page, or enter a range such as 1-4,7.",
			},
		},
		Validate: validateRotateParams,
		Run:      runRotate,
	})
}

// validateRotateParams rejects bad input synchronously, before a job exists.
func validateRotateParams(raw json.RawMessage) error {
	p, err := parseRotateParams(raw)
	if err != nil {
		return err
	}

	// pdfcpu happily accepts 45 and produces a document no printer handles
	// sensibly, so the tool restricts rotation to the three right angles the UI
	// offers.
	switch p.Angle {
	case 90, 180, 270:
	default:
		return fmt.Errorf("angle must be 90, 180 or 270, got %d", p.Angle)
	}

	if p.Pages != "" {
		if _, err := api.ParsePageSelection(p.Pages); err != nil {
			return fmt.Errorf("invalid page selection %q: %w", p.Pages, err)
		}
	}

	return nil
}

// parseRotateParams accepts the angle as either a number or a string, because
// the catalog declares it as an enum and enums arrive from the frontend select
// as strings.
func parseRotateParams(raw json.RawMessage) (rotateParams, error) {
	var wire struct {
		Angle json.RawMessage `json:"angle"`
		Pages string          `json:"pages"`
	}
	if len(raw) == 0 {
		return rotateParams{}, fmt.Errorf("rotate requires an angle")
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return rotateParams{}, fmt.Errorf("invalid rotate parameters: %w", err)
	}

	p := rotateParams{Pages: wire.Pages}
	// An explicit null is the same as an absent key. Unmarshalling null into an
	// int succeeds and silently leaves 0, which would surface downstream as the
	// confusing "angle must be 90, 180 or 270, got 0".
	if len(wire.Angle) == 0 || string(wire.Angle) == "null" {
		return p, fmt.Errorf("rotate requires an angle")
	}

	if err := json.Unmarshal(wire.Angle, &p.Angle); err == nil {
		return p, nil
	}

	var asString string
	if err := json.Unmarshal(wire.Angle, &asString); err != nil {
		return p, fmt.Errorf("angle must be a number, got %s", string(wire.Angle))
	}
	// Atoi rather than Sscanf: Sscanf("90abc", "%d") succeeds with 90 and
	// discards the rest, accepting input the caller clearly got wrong.
	angle, err := strconv.Atoi(asString)
	if err != nil {
		return p, fmt.Errorf("angle must be a number, got %q", asString)
	}
	p.Angle = angle
	return p, nil
}

func runRotate(ctx context.Context, job *jobs.Job, reporter *jobs.Reporter) error {
	p, err := parseRotateParams(job.Params)
	if err != nil {
		return jobs.Permanent(err)
	}
	if err := validateRotateParams(job.Params); err != nil {
		return jobs.Permanent(err)
	}

	return RunDerive(ctx, job, reporter, func(ctx context.Context, workDir string, inputs []DeriveInput, reporter *jobs.Reporter) (*DeriveResult, error) {
		in := inputs[0]
		outPath := filepath.Join(workDir, "rotated.pdf")

		var selection []string
		if p.Pages != "" {
			selection, err = api.ParsePageSelection(p.Pages)
			if err != nil {
				return nil, jobs.Permanent(fmt.Errorf("invalid page selection %q: %w", p.Pages, err))
			}
		}

		// Object and xref streams are disabled for the same reason the upload
		// pipeline disables them: gofpdi, which the booklet compiler uses to
		// import these pages later, cannot read them.
		conf := model.NewDefaultConfiguration()
		conf.ValidationMode = model.ValidationRelaxed
		conf.WriteObjectStream = false
		conf.WriteXRefStream = false

		reporter.Progress(0, "rotating pages")
		if err := api.RotateFile(in.LocalPath, outPath, p.Angle, selection, conf); err != nil {
			// A malformed or encrypted PDF fails the same way on every attempt.
			return nil, jobs.Permanent(fmt.Errorf("failed to rotate %s: %w", in.Name, err))
		}

		pageCount, err := api.PageCountFile(outPath)
		if err != nil {
			return nil, fmt.Errorf("failed to count pages of the rotated document: %w", err)
		}
		logger.Logf(ctx, "Rotated %s by %d degrees into %d page(s)", in.Name, p.Angle, pageCount)

		return &DeriveResult{
			OutputPath: outPath,
			Name:       DerivedName(in.Name, fmt.Sprintf("rotated %d", p.Angle)),
			// Rotation never adds, drops or reorders pages, so the derived page
			// n always comes from parent page n.
			PageSources: identityPageSources(in.DocumentID, pageCount),
		}, nil
	})
}

// identityPageSources maps every derived page to the same page of one parent.
func identityPageSources(parentID string, pages int) []PageSource {
	out := make([]PageSource, 0, pages)
	for p := 1; p <= pages; p++ {
		out = append(out, PageSource{DocumentID: parentID, Page: p})
	}
	return out
}
