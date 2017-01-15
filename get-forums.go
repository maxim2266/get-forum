package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// forum data structure
type Forum struct {
	parent   *Forum
	id       uint
	title    string
	children []*Forum
}

func main() {
	// open file
	file, err := os.Open("forums.html")

	if err != nil {
		die(err)
	}

	defer file.Close()

	// tokenizer
	z, err := TokenizerFromReader(file)

	if err != nil {
		die(err)
	}

	// print tokens
	for t := z.Next(); t != nil; t = z.Next() {
		fmt.Printf("[%s] %q -> %q\n", t.Type, string(t.Key), string(t.Value))
	}

	if z.Error != io.EOF {
		die(z.Error)
	}
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

// HTML token type
type TokenType uint32

const (
	TokenStartTag TokenType = iota
	TokenEndTag
	TokenText
	TokenComment
	TokenDoctype
	TokenAttribute
)

func (tt TokenType) String() string {
	switch tt {
	case TokenStartTag:
		return "StartTag"
	case TokenEndTag:
		return "EndTag"
	case TokenText:
		return "Text"
	case TokenComment:
		return "Comment"
	case TokenDoctype:
		return "Doctype"
	case TokenAttribute:
		return "Attribute"
	}

	return fmt.Sprintf("[unknown token type %d]", tt)
}

// HTML token
type Token struct {
	Type       TokenType
	Key, Value []byte
}

// tokenizer
type Tokenizer struct {
	tokenizer       *html.Tokenizer
	token           Token
	inAttr, inShort bool
	Error           error
}

// tokenizer constructor
func TokenizerFromReader(r io.Reader) (*Tokenizer, error) {
	reader, err := charset.NewReader(r, "utf-8")

	if err != nil {
		return nil, err
	}

	return &Tokenizer{tokenizer: html.NewTokenizer(reader)}, nil
}

// tokenizer iterator
func (z *Tokenizer) Next() *Token {
	if z.tokenizer == nil {
		return nil
	}

	if z.inAttr {
		z.token.Type = TokenAttribute
		z.token.Key, z.token.Value, z.inAttr = z.tokenizer.TagAttr()

	} else if z.inShort {
		z.inShort = false
		z.token.Type = TokenEndTag
		z.token.Key = nil
		z.token.Value, _ = z.tokenizer.TagName()

	} else {
		switch z.tokenizer.Next() {
		case html.ErrorToken:
			*z = Tokenizer{Error: z.tokenizer.Err()}
			return nil

		case html.StartTagToken:
			z.token.Type = TokenStartTag
			z.token.Key = nil
			z.token.Value, z.inAttr = z.tokenizer.TagName()

		case html.EndTagToken:
			z.token.Type = TokenEndTag
			z.token.Key = nil
			z.token.Value, _ = z.tokenizer.TagName()

		case html.SelfClosingTagToken:
			z.inShort = true
			z.token.Type = TokenStartTag
			z.token.Key = nil
			z.token.Value, z.inAttr = z.tokenizer.TagName() // can a self-closing tag have attributes?

		case html.TextToken:
			z.token = Token{
				Type:  TokenText,
				Value: z.tokenizer.Text(),
			}

		case html.CommentToken:
			z.token = Token{
				Type:  TokenComment,
				Value: z.tokenizer.Text(),
			}

		case html.DoctypeToken:
			z.token = Token{
				Type:  TokenDoctype,
				Value: z.tokenizer.Text(),
			}
		}
	}

	return &z.token
}

// find anchor tag
func findAnchor(z *html.Tokenizer) (err error) {
	for {
		switch z.Next() {
		case html.ErrorToken:
			if err = z.Err(); err == io.EOF {
				err = errors.New("Unexpected end of input")
			}

			return

		case html.StartTagToken:
			tag, hasAttr := z.TagName()

			if bytes.Compare(tag, []byte("div")) == 0 && hasAttr && hasAttrValue(z, []byte("id"), []byte("f-map")) {
				return
			}
		}
	}
}

// check if the current opening tag has the specified attribute with the given value
func hasAttrValue(z *html.Tokenizer, attr, val []byte) bool {
	k, v, more := z.TagAttr()
	found := bytes.Compare(k, attr) == 0 && bytes.Compare(v, val) == 0

	for !found && more {
		k, v, more = z.TagAttr()
		found = bytes.Compare(k, attr) == 0 && bytes.Compare(v, val) == 0
	}

	return found
}

// error handling
func die(err error) {
	os.Stderr.WriteString("ERROR: " + err.Error() + "\n")
	os.Exit(1)
}
