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
	"strings"
	"testing"

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

func makeOOXMLBytes(t *testing.T, entryPath string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		entryPath:             `<root/>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
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
	docxBytes := makeOOXMLBytes(t, "word/document.xml")
	xlsxBytes := makeOOXMLBytes(t, "xl/workbook.xml")
	pptxBytes := makeOOXMLBytes(t, "ppt/presentation.xml")

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

func TestPrepareAvatarUploadRejectsOversizedDimensions(t *testing.T) {
	// 4097 pixels exceeds maxDecodeDimension; the header is tiny, so the guard must
	// reject before a full decode can allocate a large image.
	img := image.NewRGBA(image.Rect(0, 0, maxDecodeDimension+1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	_, err := prepareAvatarUpload("wide.png", bytes.NewReader(buf.Bytes()), 2<<20)
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != ErrCodeInvalidImage {
		t.Fatalf("oversized avatar error = %v, want %s", err, ErrCodeInvalidImage)
	}
}

func TestPrepareAvatarUploadRejectsNonImagesAndDocuments(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4\n%%EOF\n")
	if prepared, err := prepareAvatarUpload("doc.pdf", bytes.NewReader(pdfBytes), 2<<20); err == nil {
		prepared.Close()
		t.Fatal("expected PDF to be rejected for avatar upload")
	}

	// Extra configured MIME types must never allow SVG.
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
}
