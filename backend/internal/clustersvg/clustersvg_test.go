package clustersvg

import "testing"

func almostEq(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if got < want-tol || got > want+tol {
		t.Errorf("%s = %v, want %v (+/- %v)", name, got, want, tol)
	}
}

func TestAncestorGroupTransformAppliedToRects(t *testing.T) {
	svg := `
		<svg viewBox="0 0 600 800" xmlns="http://www.w3.org/2000/svg">
			<g id="r1 r2 r3 r4" transform="translate(-420 75) scale(1.75)">
				<g id="r1">
					<text font-weight="bold" font-size="20" y="45.66848" x="378.113" fill="#cccccc">R01</text>
					<rect fill="#e5e5e5" stroke="#7f7f7f" x="553.16835" y="20.49957" width="16" height="20" id="c1r1p1"/>
					<image xlink:href="" x="553.16835" y="20.49957" width="16" height="20" id="c1r1p1"/>
				</g>
			</g>
		</svg>
	`
	layout := Parse(svg)
	almostEq(t, "viewBoxWidth", layout.ViewBoxWidth, 600, 0.001)
	almostEq(t, "viewBoxHeight", layout.ViewBoxHeight, 800, 0.001)
	if len(layout.Seats) != 1 {
		t.Fatalf("seats = %d, want 1", len(layout.Seats))
	}
	seat := layout.Seats[0]
	if seat.Host != "c1r1p1" {
		t.Errorf("host = %q, want c1r1p1", seat.Host)
	}
	almostEq(t, "x", seat.X, 548.0446, 0.01)
	almostEq(t, "y", seat.Y, 110.8742, 0.01)
	almostEq(t, "width", seat.Width, 28, 0.01)
	almostEq(t, "height", seat.Height, 35, 0.01)

	if len(layout.RowLabels) != 1 || layout.RowLabels[0].Text != "R01" {
		t.Errorf("rowLabels = %+v, want single R01", layout.RowLabels)
	}
}

func TestMatrixTransformDirectlyOnElement(t *testing.T) {
	svg := `
		<svg viewBox="0 0 600 800" xmlns="http://www.w3.org/2000/svg">
			<g class="r1">
				<g class="r1-top posts">
					<rect transform="matrix(1.1671, 0, 0, 1.1671, -39.5137, -4.04201)" fill="#e5e5e5" stroke="#7f7f7f"
						x="416.56942" y="131.988" width="45.1558" height="56.44476" id="c4r1p1" />
				</g>
			</g>
			<g class="text">
				<text transform="matrix(2.06058, 0, 0, 2.06058, -251.351, -99.5144)" font-weight="bold"
					font-size="20" y="164.63968" x="171.42488" fill="#cccccc">R1</text>
			</g>
		</svg>
	`
	layout := Parse(svg)
	if len(layout.Seats) != 1 {
		t.Fatalf("seats = %d, want 1", len(layout.Seats))
	}
	seat := layout.Seats[0]
	if seat.Host != "c4r1p1" {
		t.Errorf("host = %q, want c4r1p1", seat.Host)
	}
	almostEq(t, "x", seat.X, 446.6645, 0.01)
	almostEq(t, "y", seat.Y, 150.0012, 0.01)
	almostEq(t, "width", seat.Width, 52.7013, 0.01)
	almostEq(t, "height", seat.Height, 65.8767, 0.01)

	if len(layout.RowLabels) != 1 || layout.RowLabels[0].Text != "R1" {
		t.Errorf("rowLabels = %+v, want single R1", layout.RowLabels)
	}
}

func TestNonSeatRectsIgnored(t *testing.T) {
	svg := `
		<svg viewBox="0 0 600 800" xmlns="http://www.w3.org/2000/svg">
			<rect x="0" y="0" width="600" height="800" fill="#ffffff" id="background"/>
		</svg>
	`
	layout := Parse(svg)
	if len(layout.Seats) != 0 {
		t.Fatalf("seats = %d, want 0", len(layout.Seats))
	}
}
