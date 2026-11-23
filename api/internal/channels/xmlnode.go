package channels

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// node is a loose view of an XML document. The OTAs' message layouts differ in where they put a value, as an
// attribute or as an element, so readers ask for a name and take it from either.
type node struct {
	name     string
	attrs    map[string]string
	text     string
	children []*node
}

func parseXML(b []byte) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	root := &node{}
	stack := []*node{root}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			top := stack[len(stack)-1]
			top.children = append(top.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			stack[len(stack)-1].text += string(t)
		}
	}
	return root, nil
}

// all returns every descendant with the name, in document order.
func (n *node) all(name string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.name == name || name == "*" {
			out = append(out, c)
		}
		out = append(out, c.all(name)...)
	}
	return out
}

func (n *node) first(name string) *node {
	if r := n.all(name); len(r) > 0 {
		return r[0]
	}
	return nil
}

// val returns the first of the names found as an attribute of this node or of a descendant, or as the text of a descendant.
func (n *node) val(names ...string) string {
	for _, name := range names {
		if v, ok := n.attrs[name]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	for _, name := range names {
		if f := n.firstWithValue(name); f != "" {
			return f
		}
	}
	return ""
}

func (n *node) firstWithValue(name string) string {
	for _, c := range n.children {
		if v, ok := c.attrs[name]; ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if c.name == name && strings.TrimSpace(c.text) != "" {
			return strings.TrimSpace(c.text)
		}
		if v := c.firstWithValue(name); v != "" {
			return v
		}
	}
	return ""
}
