package files

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // Register GIF decoder for avatar re-encoding.
	_ "image/jpeg" // Register JPEG decoder for avatar re-encoding.
	"image/png"
	"io"
	"mime"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gabriel-vasile/mimetype"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Register WebP decoder for avatar re-encoding.
)

const (
	mimeJPEG = "image/jpeg"
	mimePNG  = "image/png"
	mimeWebP = "image/webp"
	mimeGIF  = "image/gif"
	mimePDF  = "application/pdf"
	mimeTXT  = "text/plain"
	mimeCSV  = "text/csv"
	mimeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	mimeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	mimePPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

	maxAvatarEdgePixels  = 512
	maxDecodeDimension   = 4096
	maxDecodeTotalPixels = 16_000_000
	maxOriginalNameRunes = 255
)

type mimeRule struct {
	canonicalExt string
	allowedExts  []string
	isImage      bool
}

var builtinMIMETypes = map[string]mimeRule{
	mimeJPEG: {canonicalExt: ".jpg", allowedExts: []string{".jpg", ".jpeg"}, isImage: true},
	mimePNG:  {canonicalExt: ".png", allowedExts: []string{".png"}, isImage: true},
	mimeWebP: {canonicalExt: ".webp", allowedExts: []string{".webp"}, isImage: true},
	mimeGIF:  {canonicalExt: ".gif", allowedExts: []string{".gif"}, isImage: true},
	mimePDF:  {canonicalExt: ".pdf", allowedExts: []string{".pdf"}},
	mimeTXT:  {canonicalExt: ".txt", allowedExts: []string{".txt", ".log", ".md"}},
	mimeCSV:  {canonicalExt: ".csv", allowedExts: []string{".csv"}},
	mimeDOCX: {canonicalExt: ".docx", allowedExts: []string{".docx"}},
	mimeXLSX: {canonicalExt: ".xlsx", allowedExts: []string{".xlsx"}},
	mimePPTX: {canonicalExt: ".pptx", allowedExts: []string{".pptx"}},
}

// forbiddenMIMETypes are always rejected regardless of extra configuration to prevent stored XSS.
var forbiddenMIMETypes = map[string]struct{}{
	"image/svg+xml":            {},
	"text/html":                {},
	"application/xhtml+xml":    {},
	"application/javascript":   {},
	"text/javascript":          {},
	"application/x-javascript": {},
	"text/xml":                 {},
	"application/xml":          {},
}

var forbiddenExts = map[string]struct{}{
	".svg":   {},
	".svgz":  {},
	".html":  {},
	".htm":   {},
	".xhtml": {},
	".js":    {},
	".mjs":   {},
	".cjs":   {},
	".xml":   {},
}

type preparedUpload struct {
	file         *os.File
	path         string
	originalName string
	mimeType     string
	ext          string
	sizeBytes    int64
	sha256       string
}

func (p *preparedUpload) Close() {
	if p == nil {
		return
	}
	if p.file != nil {
		_ = p.file.Close()
		p.file = nil
	}
	if p.path != "" {
		_ = os.Remove(p.path)
		p.path = ""
	}
}

func isAllowedImageMIME(m string) bool {
	rule, ok := builtinMIMETypes[normalizeMIME(m)]
	return ok && rule.isImage
}

func normalizeMIME(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err != nil {
		if before, _, ok := strings.Cut(trimmed, ";"); ok {
			return strings.ToLower(strings.TrimSpace(before))
		}
		return strings.ToLower(trimmed)
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// buildAllowedExtensions returns the sorted, de-duplicated extension list accepted by the
// current allowlist so the frontend file picker can mirror backend configuration.
func buildAllowedExtensions(allowed map[string]mimeRule) []string {
	seen := make(map[string]struct{}, len(allowed))
	out := make([]string, 0, len(allowed))
	for _, rule := range allowed {
		exts := rule.allowedExts
		if len(exts) == 0 && rule.canonicalExt != "" {
			exts = []string{rule.canonicalExt}
		}
		for _, ext := range exts {
			if ext == "" {
				continue
			}
			if _, ok := seen[ext]; ok {
				continue
			}
			seen[ext] = struct{}{}
			out = append(out, ext)
		}
	}
	slices.Sort(out)
	return out
}

func buildAllowedMIMEMap(extra []string) map[string]mimeRule {
	allowed := make(map[string]mimeRule, len(builtinMIMETypes)+len(extra))
	for k, v := range builtinMIMETypes {
		allowed[k] = v
	}
	for _, raw := range extra {
		norm := normalizeMIME(raw)
		if norm == "" {
			continue
		}
		if _, blocked := forbiddenMIMETypes[norm]; blocked {
			continue
		}
		if _, exists := allowed[norm]; !exists {
			allowed[norm] = mimeRule{canonicalExt: ".bin"}
		}
	}
	return allowed
}

func bufferUploadToTemp(r io.Reader, maxBytes int64) (*os.File, string, int64, string, error) {
	if r == nil {
		return nil, "", 0, "", errMissingFile()
	}

	tmp, err := os.CreateTemp("", "tianshu-upload-*")
	if err != nil {
		return nil, "", 0, "", fmt.Errorf("create upload temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	hasher := sha256.New()
	limited := io.LimitReader(r, maxBytes+1)
	written, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		cleanup()
		return nil, "", 0, "", fmt.Errorf("read upload stream: %w", err)
	}
	if written == 0 {
		cleanup()
		return nil, "", 0, "", errEmptyFile()
	}
	if written > maxBytes {
		cleanup()
		return nil, "", 0, "", errFileTooLarge()
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, "", 0, "", fmt.Errorf("rewind upload temp file: %w", err)
	}

	return tmp, tmpPath, written, hex.EncodeToString(hasher.Sum(nil)), nil
}

func prepareGeneralUpload(
	filename string,
	r io.Reader,
	maxBytes int64,
	allowed map[string]mimeRule,
) (*preparedUpload, error) {
	tmp, tmpPath, size, sum, err := bufferUploadToTemp(r, maxBytes)
	if err != nil {
		return nil, err
	}
	p := &preparedUpload{
		file:      tmp,
		path:      tmpPath,
		sizeBytes: size,
		sha256:    sum,
	}

	ext := strings.ToLower(path.Ext(cleanBaseName(filename)))
	if _, blocked := forbiddenExts[ext]; blocked {
		p.Close()
		return nil, errFileTypeNotAllowed()
	}

	detected, err := sniffVerifiedMIME(tmp, size, ext)
	if err != nil {
		p.Close()
		return nil, err
	}
	if _, blocked := forbiddenMIMETypes[detected]; blocked {
		p.Close()
		return nil, errFileTypeNotAllowed()
	}

	rule, ok := allowed[detected]
	if !ok {
		p.Close()
		return nil, errFileTypeNotAllowed()
	}
	if ext != "" && len(rule.allowedExts) > 0 && !slices.Contains(rule.allowedExts, ext) {
		p.Close()
		return nil, errFileTypeNotAllowed()
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		p.Close()
		return nil, fmt.Errorf("rewind verified upload: %w", err)
	}

	canonicalExt := rule.canonicalExt
	if canonicalExt == "" {
		canonicalExt = ".bin"
	}
	p.mimeType = detected
	p.ext = canonicalExt
	p.originalName = sanitizeOriginalName(filename, canonicalExt)
	return p, nil
}

func prepareAvatarUpload(
	filename string,
	r io.Reader,
	maxBytes int64,
) (*preparedUpload, error) {
	tmp, tmpPath, size, _, err := bufferUploadToTemp(r, maxBytes)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	ext := strings.ToLower(path.Ext(cleanBaseName(filename)))
	if _, blocked := forbiddenExts[ext]; blocked {
		return nil, errFileTypeNotAllowed()
	}

	detected, err := sniffVerifiedMIME(tmp, size, ext)
	if err != nil {
		return nil, err
	}
	rule, ok := builtinMIMETypes[detected]
	if !ok || !rule.isImage {
		return nil, errFileTypeNotAllowed()
	}
	if ext != "" && !slices.Contains(rule.allowedExts, ext) {
		return nil, errFileTypeNotAllowed()
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind avatar temp file: %w", err)
	}
	cfg, _, err := image.DecodeConfig(tmp)
	if err != nil {
		return nil, errInvalidImage(err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > maxDecodeDimension || cfg.Height > maxDecodeDimension ||
		int64(cfg.Width)*int64(cfg.Height) > maxDecodeTotalPixels {
		return nil, errInvalidImage(errors.New("avatar dimensions exceed safety bounds"))
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind avatar for decode: %w", err)
	}
	src, _, err := image.Decode(tmp)
	if err != nil {
		return nil, errInvalidImage(err)
	}

	// Re-encode onto a fresh RGBA canvas (resized to <=512px) to strip all EXIF/GPS/XMP metadata.
	dst := normalizeAvatarImage(src)

	outFile, err := os.CreateTemp("", "tianshu-avatar-*")
	if err != nil {
		return nil, fmt.Errorf("create avatar output temp file: %w", err)
	}
	outPath := outFile.Name()
	cleanupOut := func() {
		_ = outFile.Close()
		_ = os.Remove(outPath)
	}

	hasher := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(outFile, hasher)}
	if err := png.Encode(counter, dst); err != nil {
		cleanupOut()
		return nil, fmt.Errorf("encode avatar png: %w", err)
	}
	if _, err := outFile.Seek(0, io.SeekStart); err != nil {
		cleanupOut()
		return nil, fmt.Errorf("rewind encoded avatar: %w", err)
	}

	cleanName := sanitizeOriginalName(filename, ".png")
	if curExt := path.Ext(cleanName); curExt != "" && !strings.EqualFold(curExt, ".png") {
		cleanName = strings.TrimSuffix(cleanName, curExt) + ".png"
	} else if curExt == "" {
		cleanName += ".png"
	}
	cleanName = sanitizeOriginalName(cleanName, ".png")

	return &preparedUpload{
		file:         outFile,
		path:         outPath,
		originalName: cleanName,
		mimeType:     mimePNG,
		ext:          ".png",
		sizeBytes:    counter.n,
		sha256:       hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func normalizeAvatarImage(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	dstW, dstH := w, h
	if w > maxAvatarEdgePixels || h > maxAvatarEdgePixels {
		if w >= h {
			dstW = maxAvatarEdgePixels
			dstH = max(1, (h*maxAvatarEdgePixels+w/2)/w)
		} else {
			dstH = maxAvatarEdgePixels
			dstW = max(1, (w*maxAvatarEdgePixels+h/2)/h)
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	return dst
}

func sniffVerifiedMIME(f *os.File, size int64, ext string) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek for mime sniff: %w", err)
	}
	mt, err := mimetype.DetectReader(f)
	if err != nil {
		return "", fmt.Errorf("detect mime type: %w", err)
	}
	detected := normalizeMIME(mt.String())

	// Reject embedded SVG/HTML disguised inside plain text or XML.
	if detected == mimeTXT || detected == mimeCSV || detected == "text/xml" || detected == "application/xml" {
		if hasActiveMarkupPrefix(f) {
			return "text/html", nil
		}
		if detected == mimeTXT && ext == ".csv" {
			return mimeCSV, nil
		}
		if detected == mimeCSV && slices.Contains(builtinMIMETypes[mimeTXT].allowedExts, ext) {
			return mimeTXT, nil
		}
	}

	// Refine OOXML files whose [Content_Types].xml appears beyond the 3072-byte sniffing prefix.
	if detected == "application/zip" {
		if officeMIME := detectOOXMLFromZip(f, size); officeMIME != "" {
			return officeMIME, nil
		}
	}

	return detected, nil
}

func hasActiveMarkupPrefix(f *os.File) bool {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	sample := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	return strings.Contains(sample, "<svg") ||
		strings.Contains(sample, "<html") ||
		strings.Contains(sample, "<!doctype html") ||
		strings.Contains(sample, "<script")
}

func detectOOXMLFromZip(f *os.File, size int64) string {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return ""
	}
	var hasContentTypes, hasWord, hasXL, hasPPT bool
	for _, entry := range zr.File {
		switch entry.Name {
		case "[Content_Types].xml":
			hasContentTypes = true
		case "word/document.xml":
			hasWord = true
		case "xl/workbook.xml":
			hasXL = true
		case "ppt/presentation.xml":
			hasPPT = true
		}
	}
	if !hasContentTypes {
		return ""
	}
	switch {
	case hasWord:
		return mimeDOCX
	case hasXL:
		return mimeXLSX
	case hasPPT:
		return mimePPTX
	default:
		return ""
	}
}

func cleanBaseName(raw string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	base := path.Base(normalized)
	if base == "." || base == "/" || base == ".." {
		return ""
	}
	return base
}

func sanitizeOriginalName(raw, fallbackExt string) string {
	base := cleanBaseName(raw)
	var b strings.Builder
	b.Grow(len(base))
	for _, r := range base {
		if unicode.IsControl(r) || r == '"' {
			continue
		}
		b.WriteRune(r)
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "file" + fallbackExt
	}
	if utf8.RuneCountInString(cleaned) <= maxOriginalNameRunes {
		return cleaned
	}

	ext := path.Ext(cleaned)
	stem := strings.TrimSuffix(cleaned, ext)
	extRunes := utf8.RuneCountInString(ext)
	if extRunes >= maxOriginalNameRunes {
		return truncateRunes(cleaned, maxOriginalNameRunes)
	}
	return truncateRunes(stem, maxOriginalNameRunes-extRunes) + ext
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
