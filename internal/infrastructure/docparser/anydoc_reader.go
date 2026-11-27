package docparser

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AnydocReader converts office documents to markdown in this process, using
// the anydoc converter instead of the Python docreader service.
//
// Embedded images are rendered in place from the document model. PDFs with no
// text layer or a structure anydoc rejects are handed to the configured
// fallback reader (typically the builtin docreader). PPTX files with EMF/WMF
// media use the docreader's MarkItDown path to rasterize those embedded images.
type AnydocReader struct {
	// extractImages controls whether embedded images are parsed and placed
	// in the markdown. Off halves parse time for text-only indexing.
	extractImages bool
	// fallback handles PDFs requiring OCR/structure recovery and PPTX vector
	// media. Nil means the required conversion is reported as an error.
	fallback interfaces.DocReader
}

// NewAnydocReader builds a reader. The "anydoc_extract_images" override turns
// off image extraction. fallback may be nil.
func NewAnydocReader(overrides map[string]string, fallback interfaces.DocReader) *AnydocReader {
	return &AnydocReader{
		extractImages: !isFalsey(overrides["anydoc_extract_images"]),
		fallback:      fallback,
	}
}

// Read converts the document carried by the request.
func (r *AnydocReader) Read(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	if req.URL != "" && len(req.FileContent) == 0 {
		return nil, fmt.Errorf("anydoc engine reads uploaded documents, not URLs")
	}

	format, ok := anydoc.FormatForFile(req.FileType, req.FileName)
	if !ok {
		return nil, fmt.Errorf("anydoc engine does not support file type %q", fileTypeOf(req))
	}
	// Anydoc preserves vector bytes, but cannot rasterize EMF/WMF for the
	// browser or VLM. MarkItDown's PPTX path already provides that conversion.
	// Check the actual extension: anydoc also maps PPTM to its PPTX parser,
	// but docreader does not support PPTM.
	if r.extractImages && fileTypeOf(req) == "pptx" && pptxHasVectorMedia(req.FileContent) {
		return r.readVectorPPTX(ctx, req)
	}
	if r.extractImages && fileTypeOf(req) == "docx" && officeHasVectorMedia(req.FileContent, "word/media/") {
		return r.readVectorDOCX(ctx, req)
	}

	converted, err := anydoc.Convert(req.FileContent, anydoc.Options{
		Format:     format,
		WithAssets: r.extractImages,
	})
	if format == "pdf" && anydoc.PDFInvalidStructure(err) {
		return r.readPDFFallback(ctx, req, err, "invalid_structure")
	}
	if format == "pdf" && (anydoc.PDFNeedsOCR(err) || pdfMarkdownIsEmpty(converted, err)) {
		return r.readPDFFallback(ctx, req, err, "scanned_pdf")
	}
	if err != nil {
		return nil, fmt.Errorf("anydoc conversion failed for %q: %w", req.FileName, err)
	}
	// [local] Sparse XLSX guard.
	//
	// anydoc reads cell values, so a sheet whose real table starts a few rows
	// down yields only the sheet name: 数值总表.xlsx (13MB, 81% cell fill) came
	// back as "## 属性投放" plus an empty grid, because rows 1-3 of that sheet
	// are blank. Picture-only sheets fail the same way — the策划 workbooks put
	// their tables in screenshots, so there are no cell values to read at all.
	//
	// A healthy workbook extract is roughly 1-5% of the file size. Below 1‰
	// means the cells were never reached, so re-parse with MinerU, which works
	// from the rendered layout/OCR instead of cell values.
	if format == "xlsx" && markdownIsSparseXLSX(converted.Markdown, len(req.FileContent)) {
		return r.readXLSXFallback(ctx, req, converted.Markdown)
	}
	if converted.AssetsError != nil {
		logger.Warnf(ctx, "[anydoc] %q parsed as text but its images were dropped: %v",
			req.FileName, converted.AssetsError)
	}

	return &types.ReadResult{
		MarkdownContent: converted.Markdown,
		ImageRefs:       imageRefsFromAssets(converted.Assets),
		Metadata: map[string]string{
			"parser":         AnydocEngineName,
			"anydoc_version": anydoc.Version(),
			"source_format":  format,
		},
	}, nil
}

// Inspect only the ZIP directory: no media decompression or filesystem writes
// are needed to decide which reader can preserve the presentation's images.
func pptxHasVectorMedia(data []byte) bool {
	return officeHasVectorMedia(data, "ppt/media/")
}

func officeHasVectorMedia(data []byte, prefix string) bool {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false // Let the converter report malformed input normally.
	}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || !strings.HasPrefix(file.Name, prefix) {
			continue
		}
		switch strings.ToLower(filepath.Ext(file.Name)) {
		case ".emf", ".wmf":
			return true
		}
	}
	return false
}

// readVectorDOCX preserves the original vector bytes through MarkItDown; it
// does not promise that browsers or vision models can render those formats.
func (r *AnydocReader) readVectorDOCX(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	if !r.canFallback() {
		return nil, fmt.Errorf("anydoc conversion of %q with EMF/WMF images requires a connected docreader",
			req.FileName)
	}
	fallbackReq := *req
	fallbackReq.ParserEngine = "markitdown"
	result, err := r.fallback.Read(ctx, &fallbackReq)
	if err != nil {
		return nil, fmt.Errorf("anydoc DOCX vector-media fallback failed for %q: %w", req.FileName, err)
	}
	if result == nil {
		return nil, fmt.Errorf("anydoc DOCX vector-media fallback returned no result for %q", req.FileName)
	}
	if result.Metadata == nil {
		result.Metadata = map[string]string{}
	}
	if result.Metadata["parser"] == "" {
		result.Metadata["parser"] = "markitdown"
	}
	result.Metadata["anydoc_fallback"] = "docx_vector_media"
	return result, nil
}

func (r *AnydocReader) readVectorPPTX(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	if !r.canFallback() {
		return nil, fmt.Errorf(
			"anydoc conversion of %q with EMF/WMF images requires a connected docreader "+
				"with MarkItDown and ImageMagick",
			req.FileName)
	}
	logger.Infof(ctx, "[anydoc] %q contains EMF/WMF images, falling back to markitdown for rasterization", req.FileName)
	fallbackReq := *req
	fallbackReq.ParserEngine = "markitdown"
	result, err := r.fallback.Read(ctx, &fallbackReq)
	if err != nil {
		return nil, fmt.Errorf("anydoc PPTX vector-media fallback failed for %q: %w", req.FileName, err)
	}
	if result == nil {
		return nil, fmt.Errorf("anydoc PPTX vector-media fallback returned no result for %q", req.FileName)
	}
	if result.Metadata == nil {
		result.Metadata = map[string]string{}
	}
	if result.Metadata["parser"] == "" {
		result.Metadata["parser"] = "markitdown"
	}
	result.Metadata["anydoc_fallback"] = "pptx_vector_media"
	return result, nil
}

func pdfMarkdownIsEmpty(converted *anydoc.Result, err error) bool {
	if err != nil || converted == nil {
		return false
	}
	return strings.TrimSpace(converted.Markdown) == ""
}

func (r *AnydocReader) readPDFFallback(
	ctx context.Context, req *types.ReadRequest, convertErr error, reason string,
) (*types.ReadResult, error) {
	if !r.canFallback() {
		if convertErr != nil {
			return nil, fmt.Errorf("anydoc conversion failed for %q: %w", req.FileName, convertErr)
		}
		return nil, fmt.Errorf("anydoc conversion failed for %q: PDF has no extractable text; OCR is required", req.FileName)
	}

	logger.Infof(ctx, "[anydoc] falling back to builtin for %q: %s", req.FileName, reason)
	fallbackReq := *req
	fallbackReq.ParserEngine = BuiltinEngineName
	result, err := r.fallback.Read(ctx, &fallbackReq)
	if err != nil {
		return nil, fmt.Errorf("anydoc PDF fallback (%s) failed for %q: %w", reason, req.FileName, err)
	}
	if result == nil {
		return nil, fmt.Errorf("anydoc PDF fallback (%s) returned no result for %q", reason, req.FileName)
	}
	if result.Metadata == nil {
		result.Metadata = map[string]string{}
	}
	if result.Metadata["parser"] == "" {
		result.Metadata["parser"] = BuiltinEngineName
	}
	result.Metadata["anydoc_fallback"] = reason
	if reason == "scanned_pdf" && result.Metadata["image_source_type"] == "" {
		result.Metadata["image_source_type"] = "scanned_pdf"
	}
	return result, nil
}

// markdownIsSparseXLSX reports whether anydoc's workbook extract is too thin to
// be real content. See the call site for why 1‰ of the file size is the line.
func markdownIsSparseXLSX(markdown string, fileSize int) bool {
	if fileSize <= 0 {
		return false
	}
	return meaningfulLen(markdown) < fileSize/1000
}

// meaningfulLen counts the characters that actually carry content, skipping the
// scaffolding anydoc emits for empty cells and the HTML it wraps layouts in.
//
// A plain len() check does not work: a 43MB workbook whose sheets are all blank
// still yields ~60KB of "|  |  |  |" and "| --- |", which clears a 1‰ threshold
// while the real text is a few hundred characters. Measured on 诸天衍道.xlsx:
// 60,712 raw bytes vs 3,904 meaningful ones.
func meaningfulLen(s string) int {
	n := 0
	inTag := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case inTag:
			// HTML tag internals are markup, not content.
		case c == '|', c == '-', c == ' ', c == '\t', c == '\n', c == '\r':
			// Table pipes, separator dashes and whitespace.
		default:
			n++
		}
	}
	return n
}

// readXLSXFallback re-parses a workbook with MinerU, which reconstructs merged
// cells (rowspan/colspan) and OCRs picture-only sheets. It is strictly a
// quality upgrade: on any failure the sparse anydoc extract is returned as-is,
// so a broken MinerU never turns a readable document into a failed one.
func (r *AnydocReader) readXLSXFallback(
	ctx context.Context, req *types.ReadRequest, sparse string,
) (*types.ReadResult, error) {
	keepSparse := func() *types.ReadResult {
		return &types.ReadResult{
			MarkdownContent: sparse,
			Metadata: map[string]string{
				"parser":        AnydocEngineName,
				"source_format": "xlsx",
			},
		}
	}
	if !r.canFallback() {
		return keepSparse(), nil
	}

	logger.Infof(ctx, "[anydoc] xlsx %q extract is only %d bytes for a %d byte file, retrying with mineru",
		req.FileName, len(sparse), len(req.FileContent))

	fallbackReq := *req
	fallbackReq.ParserEngine = MinerUEngineName
	result, err := r.fallback.Read(ctx, &fallbackReq)
	if err != nil {
		logger.Warnf(ctx, "[anydoc] xlsx mineru fallback failed for %q: %v (keeping the sparse extract)",
			req.FileName, err)
		return keepSparse(), nil
	}
	if result == nil || len(result.MarkdownContent) <= len(sparse) {
		logger.Warnf(ctx, "[anydoc] xlsx mineru fallback for %q produced no more text, keeping the sparse extract",
			req.FileName)
		return keepSparse(), nil
	}
	if result.Metadata == nil {
		result.Metadata = map[string]string{}
	}
	if result.Metadata["parser"] == "" {
		result.Metadata["parser"] = MinerUEngineName
	}
	result.Metadata["anydoc_fallback"] = "xlsx_sparse"
	return result, nil
}

func (r *AnydocReader) canFallback() bool {
	if r.fallback == nil {
		return false
	}
	if connected, ok := r.fallback.(interface{ IsConnected() bool }); ok {
		return connected.IsConnected()
	}
	return true
}

// imageRefsFromAssets turns extracted assets into ImageRefs that match the
// in-place markdown links renderMarkdown wrote.
func imageRefsFromAssets(assets []anydoc.Asset) []types.ImageRef {
	if len(assets) == 0 {
		return nil
	}
	refs := make([]types.ImageRef, 0, len(assets))
	for _, asset := range assets {
		ref := anydoc.ImageDir + asset.Name
		refs = append(refs, types.ImageRef{
			Filename:    asset.Name,
			OriginalRef: ref,
			MimeType:    asset.MediaType,
			ImageData:   asset.Data,
		})
	}
	return refs
}

// fileTypeOf reports the request's file type, falling back to the file name's
// extension the way every reader in this package does.
func fileTypeOf(req *types.ReadRequest) string {
	if ft := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(req.FileType)), "."); ft != "" {
		return ft
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(req.FileName)), ".")
}

func isFalsey(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "no", "off":
		return true
	}
	return false
}
