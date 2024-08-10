package main

import (
	"bytes"
	"encoding/ascii85"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
)

func main() {
	imageFile, err := os.Open("letter.png")
	if err != nil {
		panic(err)
	}
	defer imageFile.Close()

	image, _, err := image.Decode(imageFile)
	if err != nil {
		panic(err)
	}

	// RGB
	imageBin := make([]byte, image.Bounds().Dx()*image.Bounds().Dy()*3)
	for y := image.Bounds().Min.Y; y < image.Bounds().Max.Y; y++ {
		for x := image.Bounds().Min.X; x < image.Bounds().Max.X; x++ {
			i := ((y-image.Bounds().Min.Y)*image.Bounds().Dx() + (x - image.Bounds().Min.X)) * 3
			r, g, b, _ := image.At(x, y).RGBA()
			imageBin[i] = byte(r >> 8)
			imageBin[i+1] = byte(g >> 8)
			imageBin[i+2] = byte(b >> 8)
		}
	}

	imageAscii85 := make([]byte, ascii85.MaxEncodedLen(len(imageBin)))
	ascii85.Encode(imageAscii85, imageBin)

	content := `
	q
	450 0 0 300 100 388 cm
	BI
	/W 1200
	/H 800
	/CS /RGB
	/BPC 8
	/F [/A85]
	ID
	` + string(imageAscii85) + `
	EI
	Q
	`

	var objs []Object

	helvetica := &ObjectFontType1{}

	catalog := &ObjectCatalog{
		Outline: &ObjectOutlines{},
		Pages: &ObjectPages{
			Kids: []Object{
				&ObjectPage{
					MediaBox: Box{0, 0, 612, 792},
					ProcSet:  &ObjectProcSet{},
					Fonts: map[string]Object{
						"F1": helvetica,
					},
					Contents: &ObjectContent{
						Content: []byte(content),
					},
				},
			},
		},
	}
	catalog.Collect(ObjectRef{}, &objs)

	w, err := os.OpenFile("output.pdf", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}

	var xrefbuf bytes.Buffer
	fmt.Fprintln(&xrefbuf, "xref")
	fmt.Fprintf(&xrefbuf, "%d %d\n", 0, len(objs)+1)
	fmt.Fprintf(&xrefbuf, "%010d %05d f\n", 0, 65535)

	var pos int
	nv, _ := fmt.Fprintln(w, "%PDF-1.3")
	pos += nv

	for _, obj := range objs {
		fmt.Fprintf(&xrefbuf, "%010d %05d n\n", pos, obj.Generation())
		var buf bytes.Buffer
		obj.Write(&buf)

		pos += buf.Len()
		io.Copy(w, &buf)
	}
	startxref := pos
	io.Copy(w, &xrefbuf)

	fmt.Fprintln(w, "trailer")
	fmt.Fprintf(w, "<< /Size %d\n", len(objs)+1)
	fmt.Fprintf(w, "/Root %d %d R\n", catalog.ID(), catalog.Generation())
	fmt.Fprintf(w, ">>\nstartxref\n%d\n%%EOF\n", startxref)
}

type Object interface {
	ID() int
	Generation() int
	Collect(ObjectRef, *[]Object)
	Write(io.Writer)
}

type objectBase struct {
	IDNum int
	Gen   int
}

func (o objectBase) ID() int {
	return o.IDNum
}

func (o objectBase) Generation() int {
	return o.Gen
}

type ObjectRef struct {
	ID  int
	Gen int
}

type ObjectCatalog struct {
	objectBase
	Outline Object
	Pages   Object
}

func (c *ObjectCatalog) Collect(parent ObjectRef, objs *[]Object) {
	*objs = append(*objs, c)
	if c.objectBase.IDNum == 0 {
		c.objectBase.IDNum = len(*objs)
	}
	p := ObjectRef{ID: c.ID(), Gen: c.Generation()}
	c.Outline.Collect(p, objs)
	c.Pages.Collect(p, objs)
}

func (c *ObjectCatalog) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", c.ID(), c.Generation())
	defer fmt.Fprintln(w, "endobj")

	fmt.Fprintln(w, "<< /Type /Catalog")
	defer fmt.Fprintln(w, ">>")

	fmt.Fprintf(w, "/Outlines %d %d R\n", c.Outline.ID(), c.Outline.Generation())
	fmt.Fprintf(w, "/Pages %d %d R\n", c.Pages.ID(), c.Pages.Generation())

}

type ObjectOutlines struct {
	objectBase
}

func (o *ObjectOutlines) Collect(_ ObjectRef, objs *[]Object) {
	*objs = append(*objs, o)
	if o.IDNum == 0 {
		o.IDNum = len(*objs)
	}
}

func (c *ObjectOutlines) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", c.ID(), c.Generation())
	defer fmt.Fprintln(w, "endobj")
	fmt.Fprintln(w, "<< /Type /Outlines")
	defer fmt.Fprintln(w, ">>")
	fmt.Fprintf(w, "/Count 0\n")
}

type ObjectPages struct {
	objectBase
	Kids []Object
}

func (p *ObjectPages) Collect(_ ObjectRef, objs *[]Object) {
	*objs = append(*objs, p)
	if p.IDNum == 0 {
		p.IDNum = len(*objs)
	}
	parent := ObjectRef{ID: p.ID(), Gen: p.Generation()}
	for _, kid := range p.Kids {
		kid.Collect(parent, objs)
	}
}

func (p *ObjectPages) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", p.ID(), p.Generation())
	defer fmt.Fprintln(w, "endobj")
	fmt.Fprintln(w, "<< /Type /Pages")
	defer fmt.Fprintln(w, ">>")
	fmt.Fprintf(w, "/Kids [")
	for _, kid := range p.Kids {
		fmt.Fprintf(w, "%d %d R ", kid.ID(), kid.Generation())
	}
	fmt.Fprintln(w, "]")
	fmt.Fprintf(w, "/Count %d\n", p.count())
}

func (p *ObjectPages) count() int {
	return len(p.Kids)
}

type ObjectPage struct {
	objectBase
	Parent   ObjectRef
	MediaBox Box
	Contents Object
	ProcSet  Object
	Fonts    map[string]Object
}

type Box struct {
	X, Y, W, H int
}

func (p *ObjectPage) Collect(parent ObjectRef, objs *[]Object) {
	p.Parent = parent
	*objs = append(*objs, p)
	if p.IDNum == 0 {
		p.IDNum = len(*objs)
	}

	parent = ObjectRef{ID: p.ID(), Gen: p.Generation()}
	p.Contents.Collect(parent, objs)
	p.ProcSet.Collect(parent, objs)
	for _, font := range p.Fonts {
		font.Collect(parent, objs)
	}
}

func (p *ObjectPage) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", p.ID(), p.Generation())
	defer fmt.Fprintln(w, "endobj")

	fmt.Fprintf(w, "<< /Type /Page")
	defer fmt.Fprintln(w, ">>")

	fmt.Fprintf(w, "/Parent %d %d R\n", p.Parent.ID, p.Parent.Gen)
	fmt.Fprintf(w, "/MediaBox [%d %d %d %d]\n", p.MediaBox.X, p.MediaBox.Y, p.MediaBox.W, p.MediaBox.H)
	fmt.Fprintf(w, "/Contents %d %d R\n", p.Contents.ID(), p.Contents.Generation())
	p.writeResource(w)
}

func (p *ObjectPage) writeResource(w io.Writer) error {
	fmt.Fprintf(w, "/Resources <<\n")
	defer fmt.Fprintln(w, ">>")
	fmt.Fprintf(w, "/ProcSet %d %d R\n", p.ProcSet.ID(), p.ProcSet.Generation())
	fmt.Fprintln(w, "/Font <<")
	// TODO: stable order is needed?
	for name, font := range p.Fonts {
		fmt.Fprintf(w, "/%s %d %d R\n", name, font.ID(), font.Generation())
	}
	fmt.Fprintln(w, ">>")
	return nil
}

type ObjectContent struct {
	objectBase
	Content []byte
}

func (c *ObjectContent) Collect(_ ObjectRef, objs *[]Object) {
	*objs = append(*objs, c)
	if c.IDNum == 0 {
		c.IDNum = len(*objs)
	}
}
func (c *ObjectContent) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", c.ID(), c.Generation())
	defer fmt.Fprintln(w, "endobj")
	fmt.Fprintf(w, "<< /Length %d >>\n", len(c.Content))
	fmt.Fprintln(w, "stream")
	w.Write(c.Content)
	fmt.Fprintln(w, "endstream")
}

type ObjectProcSet struct {
	objectBase
}

func (o *ObjectProcSet) Collect(_ ObjectRef, objs *[]Object) {
	*objs = append(*objs, o)
	if o.IDNum == 0 {
		o.IDNum = len(*objs)
	}
}

func (c *ObjectProcSet) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", c.ID(), c.Generation())
	fmt.Fprintln(w, "[/PDF /Text]")
	fmt.Fprintln(w, "endobj")
}

type ObjectFontType1 struct {
	objectBase
}

func (f *ObjectFontType1) Collect(_ ObjectRef, objs *[]Object) {
	*objs = append(*objs, f)
	if f.IDNum == 0 {
		f.IDNum = len(*objs)
	}
}
func (f *ObjectFontType1) Write(w io.Writer) {
	fmt.Fprintf(w, "%d %d obj\n", f.ID(), f.Generation())
	defer fmt.Fprintln(w, "endobj")
	fmt.Fprintln(w, "<< /Type /Font")
	defer fmt.Fprintln(w, ">>")

	fmt.Fprintln(w, "/Subtype /Type1")
	fmt.Fprintln(w, "/BaseFont /Helvetica")
	fmt.Fprintln(w, "/Encoding /WinAnsiEncoding")
}
