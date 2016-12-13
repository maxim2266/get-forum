package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

func main() {
	forums, err := readForumTree("forums.html")

	if err != nil {
		die(err)
	}

	printForums(forums)
}

// plain text print-out
func printForums(forums []*Forum) {
	for _, frm := range forums {
		fmt.Println(frm.title)

		for _, f := range frm.children {
			printForum(f, 1)
		}
	}
}

func printForum(forum *Forum, level int) {
	fmt.Printf("%s[%d]: %s\n", strings.Repeat("\t", level), forum.id, forum.title)

	for _, frm := range forum.children {
		printForum(frm, level+1)
	}
}

// tokenizer manager
func withTokenizer(fileName string, fn func(reader *html.Tokenizer) error) error {
	// open file
	file, err := os.Open(fileName)

	if err != nil {
		return err
	}

	defer file.Close()

	// wrap file into a reader for the appropriate charset
	reader, err := charset.NewReader(file, "utf-8")

	if err != nil {
		return err
	}

	// tokenizer
	tokenizer := html.NewTokenizer(reader)

	// find starting point and invoke callback
	if err = findAnchor(tokenizer); err == nil {
		err = fn(tokenizer)
	}

	// check error
	switch err {
	case nil:
		// discard the rest of the input
		_, err = io.Copy(ioutil.Discard, file) // to prevent the sending process from getting SIGPIPE
		return err
	case io.EOF:
		// all done
		return nil
	default:
		// got some error
		return err
	}
}

// forum data structure
type Forum struct {
	parent   *Forum
	id       uint
	title    string
	children []*Forum
}

func newChildForum(parent *Forum) *Forum {
	forum := &Forum{parent: parent}
	parent.children = append(parent.children, forum)
	return forum
}

// parser
func readForumTree(fileName string) ([]*Forum, error) {
	root := new(Forum)
	current := root

	// parser actions
	addChild := func(_ *html.Tokenizer) error {
		current = newChildForum(current)
		return nil
	}

	stepUp := func(_ *html.Tokenizer) error {
		current = current.parent
		return nil
	}

	cTitle := func(tkz *html.Tokenizer) (err error) {
		current.title, err = findAttribute("title", tkz)
		return
	}

	href := func(tkz *html.Tokenizer) error {
		ref, err := findAttribute("href", tkz)

		if err != nil {
			return err
		}

		id, err := strconv.ParseUint(ref, 10, 32)

		if err != nil {
			return err
		}

		current.id = uint(id)
		return nil
	}

	title := func(s string) error {
		if len(s) == 0 {
			return errors.New("Missing forum title")
		}

		current.title = s
		return nil
	}

	// parser specification
	var innerList parserFunc

	innerList = maybe("ul",
		repeat("li", seq(
			addChild,
			enter("span"),
			enterAction("a", href),
			textAction(title),
			leave,
			leave,
			rec(&innerList),
			stepUp,
		)),
	)

	spec := seq(
		textAction(nil),
		repeat("ul", seq(
			enter("li"),
			addChild,
			enter("span"),
			enterAction("span", cTitle),
			leave,
			leave,
			innerList,
			leave,
			stepUp,
			textAction(nil)),
		),
	)

	// parser invocation
	if err := withTokenizer(fileName, spec); err != nil {
		return nil, err
	}

	return root.children, nil
}

// parser is composed of functions of this type
type parserFunc func(*html.Tokenizer) error

// sequence of parsers
func seq(fns ...parserFunc) parserFunc {
	return func(tkz *html.Tokenizer) error {
		for _, fn := range fns {
			if err := fn(tkz); err != nil {
				return err
			}
		}

		return nil
	}
}

// recursive parser
func rec(pfn *parserFunc) parserFunc {
	return func(tkz *html.Tokenizer) error {
		return (*pfn)(tkz)
	}
}

// parser basic blocks
func match(tt html.TokenType, tkz *html.Tokenizer) error {
	switch t := tkz.Next(); t {
	case html.ErrorToken:
		return mapError(tkz.Err())
	case tt:
		return nil
	default:
		return fmt.Errorf("Unexpected token: %q instead of %q", t.String(), tt.String())
	}
}

func leave(tkz *html.Tokenizer) error {
	return match(html.EndTagToken, tkz)
}

// parser blocks constructors
func leaveAction(fn func() error) parserFunc {
	return func(tkz *html.Tokenizer) error {
		if err := leave(tkz); err != nil {
			return err
		}

		if fn != nil {
			return fn()
		}

		return nil
	}
}

func enter(tag string) parserFunc {
	return enterAction(tag, nil)
}

func enterAction(tag string, attrAction parserFunc) parserFunc {
	return func(tkz *html.Tokenizer) error {
		if err := match(html.StartTagToken, tkz); err != nil {
			return err
		}

		name, hasAttr := tkz.TagName()

		if string(name) != tag {
			return fmt.Errorf("Unexpected tag: %q instead of %q", string(name), tag)
		}

		if attrAction != nil {
			if !hasAttr {
				return errors.New("Missing attributes for tag " + string(name))
			}

			return attrAction(tkz)
		}

		return nil
	}
}

func repeat(tag string, fn parserFunc) parserFunc {
	return func(tkz *html.Tokenizer) error {
		for {
			switch tt := tkz.Next(); tt {
			case html.ErrorToken:
				return mapError(tkz.Err())

			case html.StartTagToken:
				name, _ := tkz.TagName()

				if string(name) != tag {
					return fmt.Errorf("Unexpected tag: %q instead of %q", string(name), tag)
				}

				if err := fn(tkz); err != nil {
					return err
				}

			case html.EndTagToken:
				return nil

			default:
				return errors.New("repeat: Unexpected token of type " + tt.String())
			}
		}
	}
}

func maybe(tag string, fn parserFunc) parserFunc {
	tail := seq(fn, leave)

	return func(tkz *html.Tokenizer) error {
		switch tt := tkz.Next(); tt {
		case html.ErrorToken:
			return mapError(tkz.Err())

		case html.StartTagToken:
			name, _ := tkz.TagName()

			if string(name) != tag {
				return fmt.Errorf("Unexpected tag: %q instead of %q", string(name), tag)
			}

			return tail(tkz)

		case html.EndTagToken:
			return nil

		default:
			return errors.New("repeat: Unexpected token of type " + tt.String())
		}
	}
}

var spacer = regexp.MustCompile(`\s+`)

func textAction(fn func(string) error) parserFunc {
	return func(tkz *html.Tokenizer) error {
		if err := match(html.TextToken, tkz); err != nil {
			return err
		}

		if text := bytes.TrimSpace(tkz.Text()); len(text) > 0 && fn != nil {
			return fn(string(spacer.ReplaceAllLiteral(text, []byte{' '})))
		}

		return nil
	}
}

// find starting point
func findAnchor(reader *html.Tokenizer) error {
	for {
		switch reader.Next() {
		case html.ErrorToken:
			return mapError(reader.Err())

		case html.StartTagToken:
			tag, hasAttr := reader.TagName()

			if hasAttr && bytes.Compare(tag, []byte("div")) == 0 {
				for {
					attr, val, more := reader.TagAttr()

					if bytes.Compare(attr, []byte("id")) == 0 && bytes.Compare(val, []byte("f-map")) == 0 {
						return nil
					}

					if !more {
						break
					}
				}
			}
		}
	}
}

// attribute extractor
func findAttribute(name string, tkz *html.Tokenizer) (string, error) {
	val, more := matchAttribute(name, tkz)

	for len(val) == 0 && more {
		val, more = matchAttribute(name, tkz)
	}

	if len(val) > 0 {
		return val, nil
	}

	return "", fmt.Errorf("Attribute %q is not found", name)
}

func matchAttribute(name string, tkz *html.Tokenizer) (string, bool) {
	att, val, more := tkz.TagAttr()

	if string(att) == name {
		return string(val), more
	}

	return "", more
}

// error handling
func mapError(err error) error {
	if err == io.EOF {
		return errors.New("Unexpected end of file")
	}

	return err
}

func die(err error) {
	os.Stderr.WriteString("ERROR: " + err.Error() + "\n")
	os.Exit(1)
}
