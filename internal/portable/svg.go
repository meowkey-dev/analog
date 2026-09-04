package portable

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// These are the drawing and presentation primitives the web SVG profile keeps.
// In particular, script, foreignObject, links, and CSS are intentionally absent:
// they can turn an otherwise visual card into an active document when the saved
// export is opened directly from disk.
var safeSVGElements = map[string]bool{
	"svg": true, "g": true, "path": true, "rect": true, "circle": true,
	"ellipse": true, "line": true, "polyline": true, "polygon": true,
	"text": true, "tspan": true, "title": true, "desc": true, "use": true,
	"image": true, "symbol": true,
	"defs": true, "lineargradient": true, "radialgradient": true, "stop": true,
	"clippath": true, "mask": true, "pattern": true, "marker": true,
	"filter": true, "fegaussianblur": true, "feoffset": true, "feblend": true,
	"fecolormatrix": true, "fecomponenttransfer": true, "fefuncr": true,
	"fefuncg": true, "fefuncb": true, "fefunca": true, "femerge": true,
	"femergenode": true, "fecomposite": true, "feflood": true,
	"feimage": true, "femorphology": true, "feturbulence": true,
	"fediffuselighting": true, "fespecularlighting": true, "fedistantlight": true,
	"fepointlight": true, "fespotlight": true, "fedisplacementmap": true,
}

var safeSVGAttributes = map[string]bool{
	"xmlns": true, "xmlns:xlink": true, "version": true, "id": true, "class": true,
	"viewbox": true, "width": true, "height": true, "x": true, "y": true,
	"x1": true, "y1": true, "x2": true, "y2": true, "cx": true, "cy": true,
	"r": true, "rx": true, "ry": true, "d": true, "points": true,
	"fill": true, "fill-opacity": true, "fill-rule": true, "stroke": true,
	"stroke-width": true, "stroke-opacity": true, "stroke-linecap": true,
	"stroke-linejoin": true, "stroke-miterlimit": true, "opacity": true,
	"transform": true, "text-anchor": true, "font-family": true, "font-size": true,
	"font-weight": true, "font-style": true, "dominant-baseline": true,
	"alignment-baseline": true, "preserveaspectratio": true, "gradientunits": true,
	"gradienttransform": true, "spreadmethod": true, "patternunits": true,
	"patterncontentunits": true, "patterntransform": true, "markerwidth": true,
	"markerheight": true, "refx": true, "refy": true, "orient": true,
	"clippathunits": true, "maskunits": true, "maskcontentunits": true,
	"filterunits": true, "primitiveunits": true, "result": true, "in": true,
	"in2": true, "mode": true, "operator": true, "k1": true, "k2": true,
	"k3": true, "k4": true, "stddeviation": true, "radius": true, "tablevalues": true,
	"values": true, "type": true, "specularconstant": true, "specularexponent": true,
	"surfacescale": true, "kernelmatrix": true, "basefrequency": true,
	"numoctaves": true, "seed": true, "scale": true, "lighting-color": true,
	"azimuth": true, "elevation": true, "limitingconeangle": true, "targetx": true,
	"targety": true, "edgemode": true, "preservealpha": true, "data-analog-stroke": true,
	"marker-start": true, "marker-mid": true, "marker-end": true, "clip-path": true,
	"mask": true, "filter": true,
}

// sanitizeSVG emits a conservative SVG subset. Invalid or non-SVG input becomes
// an empty drawing rather than falling back to executable markup.
func sanitizeSVG(input string) string {
	const empty = `<svg xmlns="http://www.w3.org/2000/svg"></svg>`
	dec := xml.NewDecoder(strings.NewReader(input))
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	depth := 0
	skip := 0
	root := false
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF && root && depth == 0 {
				if err := enc.Flush(); err == nil {
					return out.String()
				}
			}
			return empty
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if skip > 0 {
				skip++
				continue
			}
			if depth == 0 {
				if name != "svg" || root {
					return empty
				}
				root = true
			}
			if !safeSVGElements[name] {
				skip = 1
				continue
			}
			filtered := t
			filtered.Attr = filtered.Attr[:0]
			for _, attr := range t.Attr {
				if safeSVGAttribute(attr) {
					filtered.Attr = append(filtered.Attr, attr)
				}
			}
			if err := enc.EncodeToken(filtered); err != nil {
				return empty
			}
			depth++
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if depth == 0 {
				return empty
			}
			if err := enc.EncodeToken(t); err != nil {
				return empty
			}
			depth--
		case xml.CharData:
			if skip == 0 && depth > 0 {
				if err := enc.EncodeToken(t); err != nil {
					return empty
				}
			}
		case xml.Comment:
			// Comments are not needed for a portable rendering and can hide
			// confusing markup from someone reviewing the saved file.
		case xml.Directive, xml.ProcInst:
			// Do not carry declarations or processing instructions into HTML.
		}
	}
}

func safeSVGAttribute(attr xml.Attr) bool {
	name := strings.ToLower(attr.Name.Local)
	if attr.Name.Space == "http://www.w3.org/1999/xlink" && name == "href" {
		return safeHref(attr.Value)
	}
	if name == "href" || name == "xlink:href" {
		return safeHref(attr.Value)
	}
	if attr.Name.Space == "xmlns" && name == "xlink" {
		name = "xmlns:xlink"
	}
	if !safeSVGAttributes[name] {
		return false
	}
	if name == "xmlns" && attr.Value != "http://www.w3.org/2000/svg" {
		return false
	}
	if name == "xmlns:xlink" && attr.Value != "http://www.w3.org/1999/xlink" {
		return false
	}
	if strings.Contains(strings.ToLower(attr.Value), "url(") && !safeLocalURLs(attr.Value) {
		return false
	}
	return true
}

func safeHref(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "#") || safeImageDataURI(value)
}

func safeImageDataURI(value string) bool {
	if !strings.HasPrefix(strings.ToLower(value), "data:image/") {
		return false
	}
	comma := strings.IndexByte(value, ',')
	if comma < 0 {
		return false
	}
	mime := strings.ToLower(value[len("data:"):comma])
	return strings.HasPrefix(mime, "image/png") || strings.HasPrefix(mime, "image/jpeg") ||
		strings.HasPrefix(mime, "image/gif") || strings.HasPrefix(mime, "image/webp")
}

func safeLocalURLs(value string) bool {
	for {
		lower := strings.ToLower(value)
		start := strings.Index(lower, "url(")
		if start < 0 {
			return true
		}
		rest := lower[start+len("url("):]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return false
		}
		ref := strings.Trim(strings.TrimSpace(rest[:end]), "\"'")
		if !strings.HasPrefix(ref, "#") {
			return false
		}
		value = rest[end+1:]
	}
}
