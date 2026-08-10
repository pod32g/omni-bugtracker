package service

import "testing"

const svgBody = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`

// pngHead is a real PNG signature — the sniffer keys off the first 8 bytes.
var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestResolveContentTypeKeepsCorroboratedDeclaration(t *testing.T) {
	if got := ResolveContentType("image/png", pngHead); got != "image/png" {
		t.Fatalf("png declared and sniffed as png: got %q", got)
	}
	if got := ResolveContentType("application/pdf", []byte("%PDF-1.7\n1 0 obj")); got != "application/pdf" {
		t.Fatalf("pdf: got %q", got)
	}
	// Parameters must not defeat the comparison.
	if got := ResolveContentType("Text/Plain; charset=UTF-8", []byte("hello there")); got != "text/plain" {
		t.Fatalf("parameterized text/plain: got %q", got)
	}
}

// text/plain is the sniffer's "recognized nothing" answer, so it must not render in
// place — an SVG with no signature lands in exactly this bucket.
func TestPlainTextIsNotInlineSafe(t *testing.T) {
	if _, disp := DispositionFor("text/plain"); disp != "attachment" {
		t.Fatalf("text/plain served %s — it is the sniffer fallback and must download", disp)
	}
}

// The vulnerability this file exists for: an SVG is a script container, and the
// uploader chooses the Content-Type header on the multipart part.
func TestResolveContentTypeRejectsSVGHoweverDeclared(t *testing.T) {
	for _, declared := range []string{
		"image/svg+xml", // the honest claim
		"image/png",     // lying to reach the image/ prefix
		"text/html",
		"", // no claim at all
	} {
		got := ResolveContentType(declared, []byte(svgBody))
		if got == "image/svg+xml" {
			t.Fatalf("declared %q: stored as svg", declared)
		}
		if _, disp := DispositionFor(got); disp != "attachment" {
			t.Fatalf("declared %q: stored as %q, served %s — must not render", declared, got, disp)
		}
	}
}

func TestResolveContentTypeDiscardsUncorroboratedDeclaration(t *testing.T) {
	// Claiming PNG over HTML bytes must not survive.
	got := ResolveContentType("image/png", []byte("<!DOCTYPE html><html><body>hi"))
	if got == "image/png" {
		t.Fatal("html bytes stored as image/png")
	}
	if _, disp := DispositionFor(got); disp != "attachment" {
		t.Fatalf("html stored as %q, served %s", got, disp)
	}
}

func TestResolveContentTypeHandlesUnparseableAndEmpty(t *testing.T) {
	if got := ResolveContentType("not a media type", pngHead); got != "image/png" {
		t.Fatalf("garbage declaration should fall through to the sniff: got %q", got)
	}
	// Empty file: the sniffer reports text/plain, which is inline-safe and harmless.
	if got := ResolveContentType("", nil); got == "" {
		t.Fatal("empty input produced an empty content type")
	}
}

func TestDispositionForIsRetroactive(t *testing.T) {
	// Rows written before the upload-side fix carry the uploader's own string.
	for _, stored := range []string{"image/svg+xml", "text/html", "application/xml", "image/svg+xml; charset=utf-8"} {
		ct, disp := DispositionFor(stored)
		if disp != "attachment" || ct != "application/octet-stream" {
			t.Fatalf("legacy row %q served as %q/%s", stored, ct, disp)
		}
	}
	for _, stored := range []string{"image/png", "application/pdf", "image/jpeg"} {
		if ct, disp := DispositionFor(stored); disp != "inline" || ct != stored {
			t.Fatalf("safe type %q served as %q/%s", stored, ct, disp)
		}
	}
}
