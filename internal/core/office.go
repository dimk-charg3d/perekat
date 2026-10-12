package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A plain look into office documents and archives, for when nothing better
// is at hand: the text of a document with its headings and tables, the text
// of the slides, the cells of a sheet, the list of files in an archive. It
// needs no other program, so it works on a phone too.

const (
	// extractLimit caps the page made from one document.
	extractLimit = 2 << 20
	sheetRows    = 500
	archiveLimit = 3000
)

// extractHTML turns an office document into a simple page.
func extractHTML(path string) ([]byte, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("this file cannot be shown")
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	out := &page{}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		err = out.xml(files["word/document.xml"], docxRules)
	case ".pptx":
		var slides []string
		for name := range files {
			if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
				slides = append(slides, name)
			}
		}
		sort.Slice(slides, func(i, j int) bool { return digits(slides[i]) < digits(slides[j]) })
		for i, name := range slides {
			fmt.Fprintf(&out.b, `<section><div class="slide-no">%d</div>`, i+1)
			if err = out.xml(files[name], pptxRules); err != nil {
				break
			}
			out.b.WriteString("</section>")
		}
	case ".xlsx":
		err = out.sheet(files)
	case ".odt", ".odp", ".ods":
		err = out.xml(files["content.xml"], odfRules)
	default:
		err = errors.New("this file cannot be shown")
	}
	if err != nil {
		return nil, err
	}
	return []byte(out.b.String()), nil
}

func digits(name string) int {
	n, _ := strconv.Atoi(strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, filepath.Base(name)))
	return n
}

type page struct {
	b strings.Builder
}

// rules say which elements of a format become which tags, and where the
// text and the heading levels are.
type rules struct {
	block   map[string]string // element to tag
	text    string            // element whose content is text
	breaks  map[string]string // element to what it inserts
	heading func(start xml.StartElement) string
}

var docxRules = rules{
	block:  map[string]string{"p": "p", "tbl": "table", "tr": "tr", "tc": "td"},
	text:   "t",
	breaks: map[string]string{"tab": "\t", "br": "<br>"},
	heading: func(e xml.StartElement) string {
		if e.Name.Local != "pStyle" {
			return ""
		}
		for _, a := range e.Attr {
			style := strings.ToLower(a.Value)
			if a.Name.Local == "val" && (strings.HasPrefix(style, "heading") || style == "title" || strings.HasPrefix(style, "заголовок")) {
				if n := digits(style); n >= 1 && n <= 4 {
					return "h" + strconv.Itoa(n)
				}
				return "h1"
			}
		}
		return ""
	},
}

var pptxRules = rules{
	block:  map[string]string{"p": "p", "tbl": "table", "tr": "tr", "tc": "td"},
	text:   "t",
	breaks: map[string]string{"br": "<br>"},
}

var odfRules = rules{
	block:  map[string]string{"p": "p", "h": "h2", "table": "table", "table-row": "tr", "table-cell": "td", "list-item": "li", "page": "section"},
	text:   "",
	breaks: map[string]string{"tab": "\t", "line-break": "<br>", "s": " "},
}

// xml walks a document and writes its text out in the simplest tags.
func (p *page) xml(f *zip.File, r rules) error {
	if f == nil {
		return errors.New("this file cannot be shown")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	inText := 0
	// A paragraph is held back until its style says whether it is a heading.
	var para strings.Builder
	paraTag, inPara := "p", false
	w := func() *strings.Builder {
		if inPara {
			return &para
		}
		return &p.b
	}
	for p.b.Len() < extractLimit {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("this file is damaged")
		}
		switch e := tok.(type) {
		case xml.StartElement:
			name := e.Name.Local
			if r.heading != nil {
				if h := r.heading(e); h != "" {
					paraTag = h
				}
			}
			if s, ok := r.breaks[name]; ok {
				w().WriteString(s)
			}
			if name == r.text && r.text != "" {
				inText++
			}
			tag, ok := r.block[name]
			if !ok {
				continue
			}
			if tag == "p" || tag == "h2" {
				inPara, paraTag = true, tag
				para.Reset()
				continue
			}
			p.b.WriteString("<" + tag + ">")
		case xml.EndElement:
			name := e.Name.Local
			if name == r.text && r.text != "" {
				inText--
			}
			tag, ok := r.block[name]
			if !ok {
				continue
			}
			if tag == "p" || tag == "h2" {
				if inPara {
					fmt.Fprintf(&p.b, "<%s>%s</%s>", paraTag, para.String(), paraTag)
				}
				inPara = false
				continue
			}
			p.b.WriteString("</" + tag + ">")
		case xml.CharData:
			if inText > 0 || (r.text == "" && inPara) {
				w().WriteString(html.EscapeString(string(e)))
			}
		}
	}
	return nil
}

// sheet writes the first sheets of a workbook as tables.
func (p *page) sheet(files map[string]*zip.File) error {
	var shared []string
	if f := files["xl/sharedStrings.xml"]; f != nil {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		dec := xml.NewDecoder(rc)
		var cur strings.Builder
		inT := false
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch e := tok.(type) {
			case xml.StartElement:
				if e.Name.Local == "si" {
					cur.Reset()
				}
				inT = e.Name.Local == "t"
			case xml.EndElement:
				if e.Name.Local == "si" {
					shared = append(shared, cur.String())
				}
				inT = false
			case xml.CharData:
				if inT {
					cur.Write(e)
				}
			}
		}
		rc.Close()
	}
	var sheets []string
	for name := range files {
		if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
			sheets = append(sheets, name)
		}
	}
	if len(sheets) == 0 {
		return errors.New("this file cannot be shown")
	}
	sort.Slice(sheets, func(i, j int) bool { return digits(sheets[i]) < digits(sheets[j]) })
	for i, name := range sheets {
		rc, err := files[name].Open()
		if err != nil {
			return err
		}
		fmt.Fprintf(&p.b, `<section><div class="slide-no">%d</div><table>`, i+1)
		dec := xml.NewDecoder(rc)
		rows, kind, inV, col := 0, "", false, 0
		for rows < sheetRows && p.b.Len() < extractLimit {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch e := tok.(type) {
			case xml.StartElement:
				switch e.Name.Local {
				case "row":
					p.b.WriteString("<tr>")
					col = 0
				case "c":
					kind = ""
					at := col
					for _, a := range e.Attr {
						if a.Name.Local == "t" {
							kind = a.Value
						}
						if a.Name.Local == "r" {
							at = column(a.Value)
						}
					}
					// Empty cells are left out of the file; keep the columns in line.
					for ; col < at && col < 60; col++ {
						p.b.WriteString("<td></td>")
					}
					p.b.WriteString("<td>")
					col++
				case "v", "t":
					inV = true
				}
			case xml.EndElement:
				switch e.Name.Local {
				case "row":
					p.b.WriteString("</tr>")
					rows++
				case "c":
					p.b.WriteString("</td>")
				case "v", "t":
					inV = false
				}
			case xml.CharData:
				if !inV {
					continue
				}
				value := string(e)
				if kind == "s" {
					if n, err := strconv.Atoi(value); err == nil && n < len(shared) {
						value = shared[n]
					}
				}
				p.b.WriteString(html.EscapeString(value))
			}
		}
		rc.Close()
		p.b.WriteString("</table></section>")
	}
	return nil
}

// column turns a cell reference such as "AB12" into the number of its column.
func column(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A') + 1
	}
	return n - 1
}

// archiveEntries lists what is inside a zip or tar archive.
func archiveEntries(path string) ([]FSEntry, bool, error) {
	lower := strings.ToLower(path)
	var out []FSEntry
	add := func(name string, dir bool, size, modified int64) bool {
		out = append(out, FSEntry{Name: strings.TrimSuffix(name, "/"), Path: name, Dir: dir, Size: size, Modified: modified})
		return len(out) < archiveLimit
	}
	switch {
	case strings.HasSuffix(lower, ".tar"), strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		f, err := os.Open(path)
		if err != nil {
			return nil, false, plain(err)
		}
		defer f.Close()
		var r io.Reader = f
		if !strings.HasSuffix(lower, ".tar") {
			gz, err := gzip.NewReader(f)
			if err != nil {
				return nil, false, errors.New("this archive is damaged")
			}
			defer gz.Close()
			r = gz
		}
		for tr := tar.NewReader(r); ; {
			h, err := tr.Next()
			if err == io.EOF {
				return out, false, nil
			}
			if err != nil {
				return out, false, errors.New("this archive is damaged")
			}
			if !add(h.Name, h.FileInfo().IsDir(), h.Size, h.ModTime.Unix()) {
				return out, true, nil
			}
		}
	default:
		// Office documents, Android packages and many other files are zip inside.
		z, err := zip.OpenReader(path)
		if err != nil {
			return nil, false, errors.New("this archive cannot be read")
		}
		defer z.Close()
		for _, f := range z.File {
			if !add(f.Name, f.FileInfo().IsDir(), int64(f.UncompressedSize64), f.Modified.Unix()) {
				return out, true, nil
			}
		}
		return out, false, nil
	}
}
