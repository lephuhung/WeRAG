package docformat

import (
	"bytes"
	"encoding/xml"
	"io"
)

// Namespaces of the OOXML parts read here.
const (
	nsW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsA = "http://schemas.openxmlformats.org/drawingml/2006/main"
)

// node is a minimal element tree (ElementTree-like): the layout walk needs
// ordered children, descendant iteration and attribute lookup, which the
// streaming decoder alone makes awkward.
type node struct {
	space, local string
	attrs        []xml.Attr
	children     []*node
	text         string // character data directly inside this element
}

func parseXML(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var root *node
	var stack []*node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{space: t.Name.Space, local: t.Name.Local, attrs: append([]xml.Attr(nil), t.Attr...)}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return root, nil
}

// is reports whether n is the w:<local> element.
func (n *node) is(local string) bool { return n != nil && n.space == nsW && n.local == local }

// child returns the first direct w:<local> child.
func (n *node) child(local string) *node {
	if n == nil {
		return nil
	}
	for _, c := range n.children {
		if c.is(local) {
			return c
		}
	}
	return nil
}

// childrenNamed returns the direct w:<local> children.
func (n *node) childrenNamed(local string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, c := range n.children {
		if c.is(local) {
			out = append(out, c)
		}
	}
	return out
}

// walk visits n and its descendants in document order (ElementTree iter()).
func (n *node) walk(fn func(*node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.children {
		c.walk(fn)
	}
}

// descendants returns every w:<local> element under n (n included).
func (n *node) descendants(local string) []*node {
	var out []*node
	n.walk(func(x *node) {
		if x.is(local) {
			out = append(out, x)
		}
	})
	return out
}

// wattr returns the w:<local> attribute.
func (n *node) wattr(local string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.attrs {
		if a.Name.Space == nsW && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// plainAttr returns an attribute without namespace (DrawingML "typeface").
func (n *node) plainAttr(local string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}
