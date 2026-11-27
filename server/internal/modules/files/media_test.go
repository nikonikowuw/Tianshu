package files

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"tianshu/internal/shared/apperr"
)

func makePNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func makeJPEGWithFakeEXIF(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	raw := buf.Bytes()
	// Insert an APP1 EXIF segment immediately after the SOI marker (0xFF 0xD8).
	exifPayload := []byte("Exif\x00\x00SENSITIVE_GPS_COORDS_12345")
	segLen := len(exifPayload) + 2
	app1 := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}
	app1 = append(app1, exifPayload...)

	out := make([]byte, 0, len(raw)+len(app1))
	out = append(out, raw[:2]...)
	out = append(out, app1...)
	out = append(out, raw[2:]...)
	return out
}

func makeGIFBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White}
	img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return buf.Bytes()
}

// makeMinimalWebPBytes returns a valid 1x1 lossless VP8L WebP image.
func makeMinimalWebPBytes() []byte {
	return []byte{
		0x52, 0x49, 0x46, 0x46, 0x1a, 0x00, 0x00, 0x00,
		0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x4c,
		0x0d, 0x00, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00,
		0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe,
		0x07, 0x00,
	}
}

// makeMinimalBMPBytes returns a valid 1x1 24-bit Windows BMP image for testing extra image/* MIME handling.
func makeMinimalBMPBytes() []byte {
	return []byte{
		0x42, 0x4d, // "BM"
		0x3a, 0x00, 0x00, 0x00, // file size = 58
		0x00, 0x00, 0x00, 0x00, // reserved
		0x36, 0x00, 0x00, 0x00, // pixel offset = 54
		0x28, 0x00, 0x00, 0x00, // DIB header size = 40
		0x01, 0x00, 0x00, 0x00, // width = 1
		0x01, 0x00, 0x00, 0x00, // height = 1
		0x01, 0x00, // planes = 1
		0x18, 0x00, // bpp = 24
		0x00, 0x00, 0x00, 0x00, // compression = BI_RGB
		0x04, 0x00, 0x00, 0x00, // image size = 4
		0x13, 0x0b, 0x00, 0x00, // x ppm
		0x13, 0x0b, 0x00, 0x00, // y ppm
		0x00, 0x00, 0x00, 0x00, // colors
		0x00, 0x00, 0x00, 0x00, // important colors
		0xff, 0x00, 0x00, 0x00, // 1 pixel + padding
	}
}

func makeOOXMLBytes(t *testing.T, entryPath string, padBytes int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if padBytes > 0 {
		// Store uncompressed padding entry first so [Content_Types].xml appears beyond the 3072-byte sniffing prefix.
		hdr := &zip.FileHeader{Name: "docProps/padding.bin", Method: zip.Store}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip create padding: %v", err)
		}
		if _, err := w.Write(bytes.Repeat([]byte("P"), padBytes)); err != nil {
			t.Fatalf("zip write padding: %v", err)
		}
	}
	for _, entry := range []struct {
		name    string
		content string
	}{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{entryPath, `<root/>`},
	} {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatalf("zip create %s: %v", entry.name, err)
		}
		if _, err := w.Write([]byte(entry.content)); err != nil {
			t.Fatalf("zip write %s: %v", entry.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestPrepareGeneralUploadMIMETypesAndSecurityGuards(t *testing.T) {
	allowed := buildAllowedMIMEMap(nil)
	pngBytes := makePNGBytes(t, 16, 16)
	jpegBytes := makeJPEGWithFakeEXIF(t, 16, 16)
	gifBytes := makeGIFBytes(t, 8, 8)
	webpBytes := makeMinimalWebPBytes()
	pdfBytes := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")
	docxBytes := makeOOXMLBytes(t, "word/document.xml", 0)
	docxBeyondSniffPrefix := makeOOXMLBytes(t, "word/document.xml", 4096)
	xlsxBytes := makeOOXMLBytes(t, "xl/workbook.xml", 0)
	pptxBytes := makeOOXMLBytes(t, "ppt/presentation.xml", 0)

	cases := []struct {
		name     string
		filename string
		payload  []byte
		maxBytes int64
		wantMIME string
		wantCode string
	}{
		{name: "png allowed", filename: "photo.png", payload: pngBytes, maxBytes: 1 << 20, wantMIME: mimePNG},
		{name: "jpeg allowed", filename: "camera.jpg", payload: jpegBytes, maxBytes: 1 << 20, wantMIME: mimeJPEG},
		{name: "webp allowed", filename: "banner.webp", payload: webpBytes, maxBytes: 1 << 20, wantMIME: mimeWebP},
		{name: "gif allowed", filename: "anim.gif", payload: gifBytes, maxBytes: 1 << 20, wantMIME: mimeGIF},
		{name: "pdf allowed", filename: "report.pdf", payload: pdfBytes, maxBytes: 1 << 20, wantMIME: mimePDF},
		{name: "plain text allowed", filename: "notes.txt", payload: []byte("hello tianshu"), maxBytes: 1 << 20, wantMIME: mimeTXT},
		{name: "comma text with .txt allowed", filename: "data.txt", payload: []byte("a,b,c\n1,2,3\n4,5,6\n"), maxBytes: 1 << 20, wantMIME: mimeTXT},
		{name: "csv allowed", filename: "export.csv", payload: []byte("id,name\n1,alice\n"), maxBytes: 1 << 20, wantMIME: mimeCSV},
		{name: "docx allowed", filename: "spec.docx", payload: docxBytes, maxBytes: 1 << 20, wantMIME: mimeDOCX},
		{name: "docx beyond 3072-byte sniff prefix allowed", filename: "big-spec.docx", payload: docxBeyondSniffPrefix, maxBytes: 1 << 20, wantMIME: mimeDOCX},
		{name: "xlsx allowed", filename: "sheet.xlsx", payload: xlsxBytes, maxBytes: 1 << 20, wantMIME: mimeXLSX},
		{name: "pptx allowed", filename: "deck.pptx", payload: pptxBytes, maxBytes: 1 << 20, wantMIME: mimePPTX},
		{name: "svg explicitly blocked", filename: "icon.svg", payload: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), maxBytes: 1 << 20, wantCode: ErrCodeTypeNotAllowed},
		{name: "svg disguised as txt blocked", filename: "icon.txt", payload: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), maxBytes: 1 << 20, wantCode: ErrCodeTypeNotAllowed},
		{name: "html disguised as png blocked", filename: "xss.png", payload: []byte(`<!doctype html><script>alert(1)</script>`), maxBytes: 1 << 20, wantCode: ErrCodeTypeNotAllowed},
		{name: "extension mismatch blocked", filename: "fake.pdf", payload: pngBytes, maxBytes: 1 << 20, wantCode: ErrCodeTypeNotAllowed},
		{name: "empty file rejected", filename: "empty.txt", payload: nil, maxBytes: 1 << 20, wantCode: ErrCodeEmptyFile},
		{name: "file exceeding limit rejected", filename: "big.txt", payload: bytes.Repeat([]byte("a"), 128), maxBytes: 64, wantCode: ErrCodeTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := prepareGeneralUpload(tc.filename, bytes.NewReader(tc.payload), tc.maxBytes, allowed)
			if tc.wantCode != "" {
				if err == nil {
					prepared.Close()
					t.Fatalf("expected error %s, got nil", tc.wantCode)
				}
				appErr, ok := apperr.As(err)
				if !ok || appErr.Code != tc.wantCode {
					t.Fatalf("error = %v, want code %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer prepared.Close()
			if prepared.mimeType != tc.wantMIME {
				t.Fatalf("mimeType = %q, want %q", prepared.mimeType, tc.wantMIME)
			}
			if len(prepared.sha256) != 64 {
				t.Fatalf("sha256 len = %d, want 64", len(prepared.sha256))
			}
		})
	}
}

func TestExtraAllowedMIMETypesExtensionValidationAndSanitizationOrder(t *testing.T) {
	allowed := buildAllowedMIMEMap([]string{
		"application/json",
		"image/bmp",
		"application/octet-stream",
		"application/x-unknown-tianshu-type",
		"image/svg+xml",
	})
	exts := buildAllowedExtensions(allowed)

	if !slices.Contains(exts, ".json") || !slices.Contains(exts, ".bmp") {
		t.Fatalf("expected .json and .bmp in allowed extensions, got %v", exts)
	}
	if slices.Contains(exts, ".bin") || slices.Contains(exts, ".svg") {
		t.Fatalf("allowed extensions must not advertise .bin or .svg, got %v", exts)
	}

	jsonBody := []byte(`{"hello":"world"}`)
	okJSON, err := prepareGeneralUpload("config.json", bytes.NewReader(jsonBody), 1<<20, allowed)
	if err != nil {
		t.Fatalf("prepareGeneralUpload(config.json) error = %v", err)
	}
	okJSON.Close()

	// Dangerous or mismatched extensions must be rejected even for configured extra MIME types,
	// including attempts to hide forbidden extensions with trailing control characters or quotes.
	for _, badName := range []string{"dropper.exe", "run.sh", "shell.php", "xss.svg\x00", `payload.html"`} {
		if prepared, err := prepareGeneralUpload(badName, bytes.NewReader(jsonBody), 1<<20, allowed); err == nil {
			prepared.Close()
			t.Fatalf("expected filename %q to be rejected for extra MIME type", badName)
		} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeTypeNotAllowed {
			t.Fatalf("filename %q err = %v, want %s", badName, err, ErrCodeTypeNotAllowed)
		}
	}

	// Extra image/* MIME aligns Category() and IsImage() while still forcing attachment disposition.
	bmpPrepared, err := prepareGeneralUpload("diagram.bmp", bytes.NewReader(makeMinimalBMPBytes()), 1<<20, allowed)
	if err != nil {
		t.Fatalf("prepareGeneralUpload(diagram.bmp) error = %v", err)
	}
	defer bmpPrepared.Close()
	bmpFile := File{MIMEType: bmpPrepared.mimeType}
	if bmpFile.Category() != CategoryImage || !bmpFile.IsImage() {
		t.Fatalf("bmp Category=%q IsImage=%v, want image/true", bmpFile.Category(), bmpFile.IsImage())
	}
	if disp := effectiveDisposition(bmpFile.MIMEType, DispositionInline); disp != DispositionAttachment {
		t.Fatalf("effectiveDisposition(%q, inline) = %q, want attachment", bmpFile.MIMEType, disp)
	}
}

func TestHasActiveMarkupPrefixFailsClosedOnIOError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "closed-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := hasActiveMarkupPrefix(f); err == nil {
		t.Fatal("expected error when reading closed file in hasActiveMarkupPrefix")
	}
}

func TestPrepareAvatarUploadResizesAndStripsEXIF(t *testing.T) {
	rawJPEG := makeJPEGWithFakeEXIF(t, 1024, 512)
	if !bytes.Contains(rawJPEG, []byte("SENSITIVE_GPS_COORDS_12345")) {
		t.Fatal("test fixture missing fake EXIF marker")
	}

	prepared, err := prepareAvatarUpload("../../avatar.jpg", bytes.NewReader(rawJPEG), 2<<20)
	if err != nil {
		t.Fatalf("prepareAvatarUpload() error = %v", err)
	}
	defer prepared.Close()

	if prepared.mimeType != mimePNG {
		t.Fatalf("mimeType = %q, want %q", prepared.mimeType, mimePNG)
	}
	if prepared.originalName != "avatar.png" {
		t.Fatalf("originalName = %q, want avatar.png", prepared.originalName)
	}

	encoded, err := io.ReadAll(prepared.file)
	if err != nil {
		t.Fatalf("read encoded avatar: %v", err)
	}
	if bytes.Contains(encoded, []byte("SENSITIVE_GPS_COORDS_12345")) {
		t.Fatal("EXIF metadata leaked into re-encoded avatar")
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("DecodeConfig encoded avatar: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	if cfg.Width != 512 || cfg.Height != 256 {
		t.Fatalf("dimensions = %dx%d, want 512x256", cfg.Width, cfg.Height)
	}

	// Portrait image resizes height to 512 preserving aspect ratio.
	portrait, err := prepareAvatarUpload("portrait.png", bytes.NewReader(makePNGBytes(t, 512, 1024)), 2<<20)
	if err != nil {
		t.Fatalf("prepareAvatarUpload(portrait) error = %v", err)
	}
	defer portrait.Close()
	pCfg, _, err := image.DecodeConfig(portrait.file)
	if err != nil {
		t.Fatalf("DecodeConfig portrait: %v", err)
	}
	if pCfg.Width != 256 || pCfg.Height != 512 {
		t.Fatalf("portrait dimensions = %dx%d, want 256x512", pCfg.Width, pCfg.Height)
	}

	// Small image is not upscaled.
	small, err := prepareAvatarUpload("small.png", bytes.NewReader(makePNGBytes(t, 64, 48)), 2<<20)
	if err != nil {
		t.Fatalf("prepareAvatarUpload(small) error = %v", err)
	}
	defer small.Close()
	sCfg, _, err := image.DecodeConfig(small.file)
	if err != nil {
		t.Fatalf("DecodeConfig small: %v", err)
	}
	if sCfg.Width != 64 || sCfg.Height != 48 {
		t.Fatalf("small dimensions = %dx%d, want 64x48", sCfg.Width, sCfg.Height)
	}

	// Dot-only filename produces safe fallback name rather than ".png".
	dotOnly, err := prepareAvatarUpload(".jpg", bytes.NewReader(makeJPEGWithFakeEXIF(t, 32, 32)), 2<<20)
	if err != nil {
		t.Fatalf("prepareAvatarUpload(.jpg) error = %v", err)
	}
	defer dotOnly.Close()
	if dotOnly.originalName != "file.png" {
		t.Fatalf("dot-only avatar originalName = %q, want file.png", dotOnly.originalName)
	}

	// Verify WebP avatar decoding and re-encoding to PNG.
	webpAvatar, err := prepareAvatarUpload("profile.webp", bytes.NewReader(makeMinimalWebPBytes()), 2<<20)
	if err != nil {
		t.Fatalf("prepareAvatarUpload(webp) error = %v", err)
	}
	defer webpAvatar.Close()
	if webpAvatar.mimeType != mimePNG || webpAvatar.originalName != "profile.png" {
		t.Fatalf("webp avatar = (%q, %q), want (image/png, profile.png)", webpAvatar.mimeType, webpAvatar.originalName)
	}
}

func TestPrepareAvatarUploadRejectsOversizedDimensionsAndCorruptImages(t *testing.T) {
	// 4097 pixels exceeds maxDecodeDimension.
	wideImg := image.NewRGBA(image.Rect(0, 0, maxDecodeDimension+1, 1))
	var wideBuf bytes.Buffer
	if err := png.Encode(&wideBuf, wideImg); err != nil {
		t.Fatalf("png.Encode wide: %v", err)
	}
	if _, err := prepareAvatarUpload("wide.png", bytes.NewReader(wideBuf.Bytes()), 2<<20); err == nil {
		t.Fatal("expected wide avatar to be rejected")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidImage {
		t.Fatalf("wide avatar error = %v, want %s", err, ErrCodeInvalidImage)
	}

	// 4001x4001 has each edge <= 4096 but total pixels > 16_000_000 (synthetic IHDR header only, instant guard).
	ihdr4001 := []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x0f, 0xa1, // width = 4001
		0x00, 0x00, 0x0f, 0xa1, // height = 4001
		0x08, 0x06, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, // dummy crc (image/png DecodeConfig checks IHDR header bounds)
	}
	if _, err := prepareAvatarUpload("bomb.png", bytes.NewReader(ihdr4001), 2<<20); err == nil {
		t.Fatal("expected >16M pixel avatar to be rejected")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidImage {
		t.Fatalf("total-pixel bomb error = %v, want %s", err, ErrCodeInvalidImage)
	}

	// Corrupted PNG (valid magic header, truncated payload) returns 400 FILES_INVALID_IMAGE.
	corruptPNG := makePNGBytes(t, 16, 16)[:20]
	if _, err := prepareAvatarUpload("corrupt.png", bytes.NewReader(corruptPNG), 2<<20); err == nil {
		t.Fatal("expected corrupt PNG to be rejected")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidImage {
		t.Fatalf("corrupt PNG error = %v, want %s", err, ErrCodeInvalidImage)
	}
}

func TestPrepareAvatarUploadRejectsNonImagesAndSanitizesNames(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4\n%%EOF\n")
	if prepared, err := prepareAvatarUpload("doc.pdf", bytes.NewReader(pdfBytes), 2<<20); err == nil {
		prepared.Close()
		t.Fatal("expected PDF to be rejected for avatar upload")
	}

	// Control characters appended to a forbidden extension must still be blocked on avatar upload.
	if prepared, err := prepareAvatarUpload("icon.svg\x00", bytes.NewReader(makePNGBytes(t, 16, 16)), 2<<20); err == nil {
		prepared.Close()
		t.Fatal("expected .svg with trailing NUL to be rejected")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeTypeNotAllowed {
		t.Fatalf("forbidden ext with NUL error = %v, want %s", err, ErrCodeTypeNotAllowed)
	}

	// Extra configured MIME types must never allow SVG or HTML.
	allowed := buildAllowedMIMEMap([]string{"image/svg+xml", "text/html"})
	if _, ok := allowed["image/svg+xml"]; ok {
		t.Fatal("image/svg+xml must remain forbidden even when listed in extra config")
	}
	if _, ok := allowed["text/html"]; ok {
		t.Fatal("text/html must remain forbidden even when listed in extra config")
	}

	// Filename sanitization strips traversal paths and control characters.
	got := sanitizeOriginalName("..\\..\\evil\r\n\"name.png", ".png")
	if strings.ContainsAny(got, `/\"`+"\r\n") {
		t.Fatalf("sanitized name still contains unsafe chars: %q", got)
	}

	// Multi-byte UTF-8 filename longer than 255 runes is truncated cleanly while preserving extension.
	longUTF8 := strings.Repeat("天", 300) + ".pdf"
	truncated := sanitizeOriginalName(longUTF8, ".pdf")
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated UTF-8 filename is invalid UTF-8: %q", truncated)
	}
	if utf8.RuneCountInString(truncated) != maxOriginalNameRunes {
		t.Fatalf("truncated rune count = %d, want %d", utf8.RuneCountInString(truncated), maxOriginalNameRunes)
	}
	if path.Ext(truncated) != ".pdf" {
		t.Fatalf("truncated extension = %q, want .pdf", path.Ext(truncated))
	}
}
