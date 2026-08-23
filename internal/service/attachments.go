package service

import (
	"mime"
	"net/http"
	"strings"
)

// Attachment bytes are supplied by whoever can comment on an issue — which includes
// the reporter role — and are served back from the tracker's own origin, where the
// session cookie lives. So the content type is decided here, from the bytes, and
// never from what the uploader said it was.

// inlineSafe is the closed set of types the browser may render in place. Everything
// absent from it downloads. Two omissions are deliberate:
//
//   - image/svg+xml, because an SVG is a script container. Rendering one inline is a
//     stored-XSS primitive against the API origin, so it is not "an image" here no
//     matter what it claims to be.
//   - text/plain, which is less obvious: it is what http.DetectContentType returns
//     when it recognizes nothing, so it is the bucket every unfamiliar file lands in
//     — including the SVG above, which has no sniffer signature. Rendering the
//     fallback bucket inline would make the allowlist open by default and leave
//     nosniff as the only thing standing between a .svg and script execution.
//
// The cost is that a .txt or .log attachment downloads instead of previewing. That is
// the right trade against re-opening the hole this list exists to close.
var inlineSafe = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"application/pdf": true,
}

// normalizeMediaType strips parameters and lowercases, so `Text/Plain; charset=UTF-8`
// and `text/plain` compare equal. A value that will not parse has no business being
// compared against an allowlist, so it becomes the empty string and matches nothing.
func normalizeMediaType(v string) string {
	mt, _, err := mime.ParseMediaType(strings.TrimSpace(v))
	if err != nil {
		return ""
	}
	return strings.ToLower(mt)
}

// ResolveContentType decides the type an attachment will be stored and served as.
// head is the leading bytes of the file (512 is what the sniffer reads).
//
// The declared type is kept only when the bytes corroborate it; otherwise the sniff
// wins, because it is the only claim about the file that the uploader did not write.
// The practical effect is that a .docx is stored as the application/zip it really is,
// and an SVG announced as image/png is stored as the text/xml it really is — neither
// of which is inline-safe, so neither renders.
func ResolveContentType(declared string, head []byte) string {
	sniffed := normalizeMediaType(http.DetectContentType(head))
	if d := normalizeMediaType(declared); d != "" && d == sniffed {
		return d
	}
	if sniffed == "" {
		return "application/octet-stream"
	}
	return sniffed
}

// DispositionFor maps a stored content type to how it is served: the type to put on
// the wire and whether it renders in place. Non-inline responses are retyped to
// application/octet-stream rather than merely marked `attachment`, so a browser that
// mishandles the disposition still has nothing renderable to act on.
//
// This is applied on read, not on write, which is what makes it retroactive: rows
// stored before ResolveContentType existed carry an uploader-supplied type, and this
// is the check that keeps those from rendering too.
func DispositionFor(stored string) (contentType, disposition string) {
	if ct := normalizeMediaType(stored); inlineSafe[ct] {
		return ct, "inline"
	}
	return "application/octet-stream", "attachment"
}
