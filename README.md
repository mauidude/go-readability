go-readability
==============

go-readability is library for extracting the main content off of an HTML page. This library implements the readability algorithm created by arc90 labs and was heavily inspired by https://github.com/cantino/ruby-readability.

Installation
------------

`go get github.com/mauidude/go-readability`

Example
-------

```
import(
  "github.com/mauidude/go-readability"
)

...

doc, err := readability.NewDocument(html)
if err != nil {
  // do something ...
}

content := doc.Content()
// do something with my content

```


Tests
-----

To run tests
`go test github.com/mauidude/go-readability`
