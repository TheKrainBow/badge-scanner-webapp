// Package clustersvg ports mobile/app/.../api/ClusterSvg.kt: parses a 42
// intra cluster seat-map SVG into seat positions without a full SVG engine.
//
// Every cluster map is a flat sequence of <g>, <rect> and <text> tags. A
// seat's actual position comes from whichever transform currently applies,
// in one of two forms depending on the campus: inherited from an ancestor
// <g transform="translate(tx ty) scale(s)">, or carried directly as
// transform="matrix(a,0,0,d,e,f)". Both are axis-aligned scale+translate
// only — scanning tags in document order and tracking "whichever transform
// currently applies" is enough to place every seat correctly either way.
package clustersvg

import (
	"regexp"
	"strconv"
)

type Seat struct {
	Host   string  `json:"host"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type RowLabel struct {
	Text string  `json:"text"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

type Layout struct {
	ViewBoxWidth  float64    `json:"viewBoxWidth"`
	ViewBoxHeight float64    `json:"viewBoxHeight"`
	Seats         []Seat     `json:"seats"`
	RowLabels     []RowLabel `json:"rowLabels"`
}

var (
	viewBoxRegex     = regexp.MustCompile(`viewBox="[-0-9.]+\s+[-0-9.]+\s+([0-9.]+)\s+([0-9.]+)"`)
	tagRegex         = regexp.MustCompile(`<g\b[^>]*>|<rect\b[^>]*/>|<text\b[^>]*>[^<]*</text>`)
	textContentRegex = regexp.MustCompile(`>([^<]*)<`)
	seatIDRegex      = regexp.MustCompile(`^c\d+r\d+p\d+$`)
	rowLabelRegex    = regexp.MustCompile(`^R\d+$`)
	matrixRegex      = regexp.MustCompile(`matrix\(\s*([-0-9.]+)[,\s]+([-0-9.]+)[,\s]+([-0-9.]+)[,\s]+([-0-9.]+)[,\s]+([-0-9.]+)[,\s]+([-0-9.]+)\s*\)`)
	translateScaleRe = regexp.MustCompile(`translate\(\s*([-0-9.]+)[,\s]+([-0-9.]+)\s*\)\s*scale\(\s*([-0-9.]+)\s*\)`)
)

type transform struct{ a, d, e, f float64 }

var identity = transform{a: 1, d: 1}

func (t transform) x(px float64) float64 { return t.a*px + t.e }
func (t transform) y(py float64) float64 { return t.d*py + t.f }
func (t transform) w(pw float64) float64 { return t.a * pw }
func (t transform) h(ph float64) float64 { return t.d * ph }

func Parse(svg string) Layout {
	viewBoxWidth, viewBoxHeight := 600.0, 800.0
	if m := viewBoxRegex.FindStringSubmatch(svg); m != nil {
		viewBoxWidth = parseFloat(m[1], viewBoxWidth)
		viewBoxHeight = parseFloat(m[2], viewBoxHeight)
	}

	groupTransform := identity
	var seats []Seat
	var rowLabels []RowLabel

	for _, tag := range tagRegex.FindAllString(svg, -1) {
		switch {
		case len(tag) >= 2 && tag[:2] == "<g":
			if raw, ok := attr(tag, "transform"); ok {
				if t, ok := parseTransform(raw); ok {
					groupTransform = t
				}
			}
		case len(tag) >= 5 && tag[:5] == "<rect":
			id, ok := attr(tag, "id")
			if !ok || !seatIDRegex.MatchString(id) {
				continue
			}
			t := groupTransform
			if raw, ok := attr(tag, "transform"); ok {
				if parsed, ok := parseTransform(raw); ok {
					t = parsed
				}
			}
			x, ok1 := attrFloat(tag, "x")
			y, ok2 := attrFloat(tag, "y")
			w, ok3 := attrFloat(tag, "width")
			h, ok4 := attrFloat(tag, "height")
			if !ok1 || !ok2 || !ok3 || !ok4 {
				continue
			}
			seats = append(seats, Seat{Host: id, X: t.x(x), Y: t.y(y), Width: t.w(w), Height: t.h(h)})
		case len(tag) >= 5 && tag[:5] == "<text":
			if fw, _ := attr(tag, "font-weight"); fw != "bold" {
				continue
			}
			m := textContentRegex.FindStringSubmatch(tag)
			if m == nil || !rowLabelRegex.MatchString(m[1]) {
				continue
			}
			t := groupTransform
			if raw, ok := attr(tag, "transform"); ok {
				if parsed, ok := parseTransform(raw); ok {
					t = parsed
				}
			}
			x, ok1 := attrFloat(tag, "x")
			y, ok2 := attrFloat(tag, "y")
			if !ok1 || !ok2 {
				continue
			}
			rowLabels = append(rowLabels, RowLabel{Text: m[1], X: t.x(x), Y: t.y(y)})
		}
	}
	return Layout{ViewBoxWidth: viewBoxWidth, ViewBoxHeight: viewBoxHeight, Seats: seats, RowLabels: rowLabels}
}

func attrRegex(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `="([^"]*)"`)
}

// attr caches nothing (parity over micro-perf) — same "\b" reasoning as the
// Kotlin version: without it, looking up "y" could match the tail of
// font-family="...y" before the real y="..." attribute.
func attr(tag, name string) (string, bool) {
	m := attrRegex(name).FindStringSubmatch(tag)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func attrFloat(tag, name string) (float64, bool) {
	raw, ok := attr(tag, name)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	return v, err == nil
}

func parseTransform(raw string) (transform, bool) {
	if m := matrixRegex.FindStringSubmatch(raw); m != nil {
		return transform{
			a: parseFloat(m[1], 1),
			d: parseFloat(m[4], 1),
			e: parseFloat(m[5], 0),
			f: parseFloat(m[6], 0),
		}, true
	}
	if m := translateScaleRe.FindStringSubmatch(raw); m != nil {
		tx := parseFloat(m[1], 0)
		ty := parseFloat(m[2], 0)
		scale := parseFloat(m[3], 1)
		return transform{a: scale, d: scale, e: tx, f: ty}, true
	}
	return transform{}, false
}

func parseFloat(s string, fallback float64) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return v
}
