package ui

import "gopkin/core/platform"

const defaultGap float32 = 8

// RowWidget arranges children horizontally from left to right.
type RowWidget struct {
	children []Widget
	gap      float32
	padding  float32
	bounds   Rect
}

// Row creates a horizontal layout container.
func Row(children ...Widget) *RowWidget {
	return &RowWidget{
		children: filterNonNil(children),
		gap:      defaultGap,
	}
}

func (r *RowWidget) Type() string     { return "row" }
func (r *RowWidget) Bounds() Rect     { return r.bounds }
func (r *RowWidget) Children() []Widget { return r.children }

// Gap sets the horizontal spacing between children.
func (r *RowWidget) Gap(px float32) *RowWidget {
	r.gap = px
	return r
}

// Padding sets the inner padding around children.
func (r *RowWidget) Padding(px float32) *RowWidget {
	r.padding = px
	return r
}

func (r *RowWidget) Layout(bounds Rect) Rect {
	r.bounds = bounds
	n := len(r.children)
	if n == 0 {
		r.bounds.H = r.padding * 2
		return r.bounds
	}

	innerW := max(0, bounds.W-r.padding*2)
	innerH := max(0, bounds.H-r.padding*2)
	totalGap := r.gap * float32(n-1)
	cellW := max(0, (innerW-totalGap)/float32(n))

	curX := bounds.X + r.padding
	maxH := float32(0)
	for _, child := range r.children {
		occupied := child.Layout(Rect{
			X: curX,
			Y: bounds.Y + r.padding,
			W: cellW,
			H: innerH,
		})
		if occupied.H > maxH {
			maxH = occupied.H
		}
		curX += cellW + r.gap
	}

	r.bounds.H = maxH + r.padding*2
	return r.bounds
}

func (r *RowWidget) Render(driver platform.Driver) {
	for _, child := range r.children {
		child.Render(driver)
	}
}

func (r *RowWidget) HandleEvent(ev platform.Event) bool {
	for i := len(r.children) - 1; i >= 0; i-- {
		if r.children[i].HandleEvent(ev) {
			return true
		}
	}
	return false
}

// ColumnWidget arranges children vertically from top to bottom.
type ColumnWidget struct {
	children []Widget
	gap      float32
	padding  float32
	bounds   Rect
}

// Column creates a vertical layout container.
func Column(children ...Widget) *ColumnWidget {
	return &ColumnWidget{
		children: filterNonNil(children),
		gap:      defaultGap,
	}
}

func (c *ColumnWidget) Type() string       { return "column" }
func (c *ColumnWidget) Bounds() Rect       { return c.bounds }
func (c *ColumnWidget) Children() []Widget { return c.children }

// Gap sets the vertical spacing between children.
func (c *ColumnWidget) Gap(px float32) *ColumnWidget {
	c.gap = px
	return c
}

// Padding sets the inner padding around children.
func (c *ColumnWidget) Padding(px float32) *ColumnWidget {
	c.padding = px
	return c
}

func (c *ColumnWidget) Layout(bounds Rect) Rect {
	c.bounds = bounds
	if len(c.children) == 0 {
		c.bounds.H = c.padding * 2
		return c.bounds
	}

	innerW := max(0, bounds.W-c.padding*2)
	curY := bounds.Y + c.padding
	for i, child := range c.children {
		remainingH := max(0, (bounds.Y+bounds.H-c.padding)-curY)
		occupied := child.Layout(Rect{
			X: bounds.X + c.padding,
			Y: curY,
			W: innerW,
			H: remainingH,
		})
		curY += occupied.H
		if i < len(c.children)-1 {
			curY += c.gap
		}
	}

	c.bounds.H = (curY - bounds.Y) + c.padding
	return c.bounds
}

func (c *ColumnWidget) Render(driver platform.Driver) {
	for _, child := range c.children {
		child.Render(driver)
	}
}

func (c *ColumnWidget) HandleEvent(ev platform.Event) bool {
	for i := len(c.children) - 1; i >= 0; i-- {
		if c.children[i].HandleEvent(ev) {
			return true
		}
	}
	return false
}

// CenterWidget centers a single child horizontally and vertically within its bounds.
type CenterWidget struct {
	child  Widget
	bounds Rect
}

// Center creates a layout node that centers child within the available space.
func Center(child Widget) *CenterWidget {
	return &CenterWidget{child: child}
}

func (c *CenterWidget) Type() string  { return "center" }
func (c *CenterWidget) Bounds() Rect  { return c.bounds }
func (c *CenterWidget) Child() Widget { return c.child }

func (c *CenterWidget) Layout(bounds Rect) Rect {
	c.bounds = bounds
	if c.child == nil {
		return c.bounds
	}

	measured := c.child.Layout(Rect{X: 0, Y: 0, W: bounds.W, H: bounds.H})
	offsetX := bounds.X + max(0, (bounds.W-measured.W)/2)
	offsetY := bounds.Y + max(0, (bounds.H-measured.H)/2)
	c.child.Layout(Rect{
		X: offsetX,
		Y: offsetY,
		W: measured.W,
		H: measured.H,
	})
	return c.bounds
}

func (c *CenterWidget) Render(driver platform.Driver) {
	if c.child != nil {
		c.child.Render(driver)
	}
}

func (c *CenterWidget) HandleEvent(ev platform.Event) bool {
	if c.child != nil {
		return c.child.HandleEvent(ev)
	}
	return false
}

// GridWidget arranges children in a fixed-column grid.
type GridWidget struct {
	cols     int
	children []Widget
	gap      float32
	padding  float32
	bounds   Rect
}

// Grid creates a grid layout container with cols columns.
func Grid(cols int, children ...Widget) *GridWidget {
	if cols <= 0 {
		cols = 1
	}
	return &GridWidget{
		cols:     cols,
		children: filterNonNil(children),
		gap:      defaultGap,
	}
}

func (g *GridWidget) Type() string       { return "grid" }
func (g *GridWidget) Bounds() Rect       { return g.bounds }
func (g *GridWidget) Cols() int          { return g.cols }
func (g *GridWidget) Children() []Widget { return g.children }

// Gap sets the horizontal and vertical spacing between grid cells.
func (g *GridWidget) Gap(px float32) *GridWidget {
	g.gap = px
	return g
}

// Padding sets the inner padding around the grid.
func (g *GridWidget) Padding(px float32) *GridWidget {
	g.padding = px
	return g
}

func (g *GridWidget) Layout(bounds Rect) Rect {
	g.bounds = bounds
	n := len(g.children)
	if n == 0 {
		g.bounds.H = g.padding * 2
		return g.bounds
	}

	innerW := max(0, bounds.W-g.padding*2)
	totalColGap := g.gap * float32(g.cols-1)
	cellW := max(0, (innerW-totalColGap)/float32(g.cols))

	rows := (n + g.cols - 1) / g.cols
	curY := bounds.Y + g.padding

	for row := range rows {
		rowMaxH := float32(0)
		remainingH := max(0, (bounds.Y+bounds.H-g.padding)-curY)

		for col := range g.cols {
			idx := row*g.cols + col
			if idx >= n {
				break
			}
			cellX := bounds.X + g.padding + float32(col)*(cellW+g.gap)
			occupied := g.children[idx].Layout(Rect{
				X: cellX,
				Y: curY,
				W: cellW,
				H: remainingH,
			})
			if occupied.H > rowMaxH {
				rowMaxH = occupied.H
			}
		}

		curY += rowMaxH
		if row < rows-1 {
			curY += g.gap
		}
	}

	g.bounds.H = (curY - bounds.Y) + g.padding
	return g.bounds
}

func (g *GridWidget) Render(driver platform.Driver) {
	for _, child := range g.children {
		child.Render(driver)
	}
}

func (g *GridWidget) HandleEvent(ev platform.Event) bool {
	for i := len(g.children) - 1; i >= 0; i-- {
		if g.children[i].HandleEvent(ev) {
			return true
		}
	}
	return false
}

func filterNonNil(children []Widget) []Widget {
	out := make([]Widget, 0, len(children))
	for _, ch := range children {
		if ch != nil {
			out = append(out, ch)
		}
	}
	return out
}

