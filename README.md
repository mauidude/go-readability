# go-readability

go-readability is library for extracting the main content off of an HTML page. This library implements the readability algorithm created by arc90 labs and was heavily inspired by https://github.com/cantino/ruby-readability.

## Installation

`go install github.com/mauidude/go-readability`

## CLI Tool

You can run readability via the command line to extract content from a single HTML file by running the following command:

```bash
$ readability path/to/file.html
```

For help with usage and options you can run the following:

```bash
$ readability --help
```

## Example

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


## Tests

To run tests
`go test github.com/mauidude/go-readability`
