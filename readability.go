package readability

import (
	"bytes"
	"code.google.com/p/go.net/html"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"log"
	"math"
	"regexp"
	"strings"
)

var replaceBrsRegexp = regexp.MustCompile(`(?i)(<br[^>]*>[ \n\r\t]*){2,}`)
var replaceFontsRegexp = regexp.MustCompile(`(?i)<(\/?)font[^>]*>`)

var blacklistCandidatesRegexp = regexp.MustCompile(`(?i)popupbody`)
var okMaybeItsACandidateRegexp = regexp.MustCompile(`(?i)and|article|body|column|main|shadow`)
var unlikelyCandidatesRegexp = regexp.MustCompile(`(?i)combx|comment|community|disqus|extra|foot|header|menu|remark|rss|shoutbox|sidebar|sponsor|ad-break|agegate|pagination|pager|popup`)
var divToPElementsRegexp = regexp.MustCompile(`(?i)<(a|blockquote|dl|div|img|ol|p|pre|table|ul)`)

var negativeRegexp = regexp.MustCompile(`(?i)combx|comment|com-|contact|foot|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|shoutbox|sidebar|sponsor|shopping|tags|tool|widget`)
var positiveRegexp = regexp.MustCompile(`(?i)rticle|body|content|entry|hentry|main|page|pagination|post|text|blog|story`)

var normalizeWhitespaceRegexp = regexp.MustCompile(`[\r\n\f]+`)

type candidate struct {
	selection *goquery.Selection
	score     float32
}

func (c *candidate) Node() *html.Node {
	return c.selection.Get(0)
}

type Document struct {
	input         string
	document      *goquery.Document
	content       string
	candidates    map[*html.Node]*candidate
	bestCandidate *candidate

	RemoveUnlikelyCandidates bool
	WeightClasses            bool
	CleanConditionally       bool
	BestCandidateHasImage    bool
	RetryLength              int
	MinTextLength            int
	RemoveEmptyNodes         bool
	WhitelistTags            []string
}

func NewDocument(s string) (*Document, error) {
	d := &Document{
		input:                    s,
		WhitelistTags:            []string{"div", "p"},
		RemoveUnlikelyCandidates: true,
		WeightClasses:            true,
		CleanConditionally:       true,
		RetryLength:              250,
		MinTextLength:            25,
		RemoveEmptyNodes:         true,
	}
	err := d.initializeHtml(s)
	if err != nil {
		return nil, err
	}

	return d, nil
}

func (d *Document) initializeHtml(s string) error {
	// replace consecutive <br>'s with p tags
	s = replaceBrsRegexp.ReplaceAllString(s, "</p><p>")

	// replace font tags
	s = replaceFontsRegexp.ReplaceAllString(s, "<$1span>")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return err
	}

	// if no body (like from a redirect or empty string)
	if doc.Find("body").Length() == 0 {
		s = "<body/>"
		return d.initializeHtml(s)
	}

	traverse(doc.Get(0), func(n *html.Node) {
		if n.Type == html.CommentNode {
			parent := n.Parent
			if parent != nil {
				parent.RemoveChild(n)
			}
		}
	})

	d.document = doc
	return nil
}

func (d *Document) Content() string {
	if d.content == "" {
		d.prepareCandidates()

		article := d.getArticle()
		articleText := d.sanitize(article)

		if len(strings.TrimSpace(articleText)) < d.RetryLength {
			if d.RemoveUnlikelyCandidates {
				d.RemoveUnlikelyCandidates = false
			} else if d.WeightClasses {
				d.WeightClasses = false
			} else if d.CleanConditionally {
				d.CleanConditionally = false
			} else {
				d.content = articleText
			}

			d.initializeHtml(d.input)
			d.content = d.Content()
		}

		d.content = articleText
	}

	return d.content
}

func (d *Document) prepareCandidates() {
	d.document.Find("script, style").Each(func(i int, s *goquery.Selection) {
		removeNodes(s)
	})

	if d.RemoveUnlikelyCandidates {
		d.removeUnlikelyCandidates()
	}

	d.transformMisusedDivsIntoParagraphs()
	d.scoreParagraphs(d.MinTextLength)
	d.selectBestCandidate()
}

func (d *Document) selectBestCandidate() {
	var best *candidate

	for _, c := range d.candidates {
		if best == nil {
			best = c
		} else if best.score < c.score {
			best = c
		}
	}

	if best == nil {
		best = &candidate{d.document.Find("body"), 0}
	}

	d.bestCandidate = best
}

func (d *Document) getArticle() string {
	output := bytes.NewBufferString("<div>")

	siblingScoreThreshold := float32(math.Max(10, float64(d.bestCandidate.score*.2)))

	d.bestCandidate.selection.Siblings().Union(d.bestCandidate.selection).Each(func(i int, s *goquery.Selection) {
		append := false
		n := s.Get(0)

		if n == d.bestCandidate.Node() {
			append = true
		} else if c, ok := d.candidates[n]; ok && c.score >= siblingScoreThreshold {
			append = true
		}

		if s.Is("p") {
			linkDensity := d.getLinkDensity(s)
			content := s.Text()
			contentLength := len(content)

			if contentLength >= 80 && linkDensity < .25 {
				append = true
			} else if contentLength < 80 && linkDensity == 0 {
				append, _ = regexp.MatchString(`\.( |$)`, content)
			}
		}

		if append {
			tag := "div"
			if s.Is("p") {
				tag = n.Data
			}

			html, _ := s.Html()
			fmt.Fprintf(output, "<%s>%s</%s>", tag, html, tag)
		}
	})

	output.Write([]byte("</div>"))

	return output.String()
}

func (d *Document) removeUnlikelyCandidates() {
	d.document.Find("*").Not("html,body").Each(func(i int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		id, _ := s.Attr("id")

		str := class + id

		if blacklistCandidatesRegexp.MatchString(str) || (unlikelyCandidatesRegexp.MatchString(str) && !okMaybeItsACandidateRegexp.MatchString(str)) {
			log.Printf("Removing unlikely candidate -%s\n", str)
			removeNodes(s)
		}
	})
}

func (d *Document) transformMisusedDivsIntoParagraphs() {
	d.document.Find("div").Each(func(i int, s *goquery.Selection) {
		html, err := s.Html()
		if err != nil {
			log.Printf("Unable to transform div to p %s\n", err)
			return
		}

		// transform <div>s that do not contain other block elements into <p>s
		if !divToPElementsRegexp.MatchString(html) {
			class, _ := s.Attr("class")
			id, _ := s.Attr("id")
			log.Printf("Altering div(#%s.%s) to p\n", id, class)

			node := s.Get(0)
			node.Data = "p"
		}
	})
}

func (d *Document) scoreParagraphs(minimumTextLength int) {
	candidates := make(map[*html.Node]*candidate)

	d.document.Find("p,td").Each(func(i int, s *goquery.Selection) {
		text := s.Text()

		// if this paragraph is less than x chars, don't count it
		if len(text) < minimumTextLength {
			return
		}

		parent := s.Parent()
		parentNode := parent.Get(0)

		grandparent := parent.Parent()
		var grandparentNode *html.Node
		if grandparent.Length() > 0 {
			grandparentNode = grandparent.Get(0)
		}

		if _, ok := candidates[parentNode]; !ok {
			candidates[parentNode] = d.scoreNode(parent)
		}
		if grandparentNode != nil {
			if _, ok := candidates[grandparentNode]; !ok {
				candidates[grandparentNode] = d.scoreNode(grandparent)
			}
		}

		contentScore := float32(1.0)
		contentScore += float32(strings.Count(text, ",") + 1)
		contentScore += float32(math.Min(float64(int(len(text)/100.0)), 3))

		candidates[parentNode].score += contentScore
		if grandparentNode != nil {
			candidates[grandparentNode].score += contentScore / 2.0
		}
	})

	// scale the final candidates score based on link density. Good content
	// should have a relatively small link density (5% or less) and be mostly
	// unaffected by this operation
	for _, candidate := range candidates {
		candidate.score = candidate.score * (1 - d.getLinkDensity(candidate.selection))
	}

	d.candidates = candidates
}

func (d *Document) getLinkDensity(s *goquery.Selection) float32 {
	linkLength := len(s.Find("a").Text())
	textLength := len(s.Text())

	if textLength == 0 {
		return 0
	}

	return float32(linkLength) / float32(textLength)
}

func (d *Document) classWeight(s *goquery.Selection) int {
	weight := 0
	if !d.WeightClasses {
		return weight
	}

	class, _ := s.Attr("class")
	id, _ := s.Attr("id")

	if class != "" {
		if negativeRegexp.MatchString(class) {
			weight -= 25
		}

		if positiveRegexp.MatchString(class) {
			weight += 25
		}
	}

	if id != "" {
		if negativeRegexp.MatchString(id) {
			weight -= 25
		}

		if positiveRegexp.MatchString(id) {
			weight += 25
		}
	}

	return weight
}

func (d *Document) scoreNode(s *goquery.Selection) *candidate {
	contentScore := d.classWeight(s)
	if s.Is("div") {
		contentScore += 5
	} else if s.Is("blockquote,form") {
		contentScore = 3
	} else if s.Is("th") {
		contentScore -= 5
	}

	return &candidate{s, float32(contentScore)}
}

func (d *Document) sanitize(article string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(article))
	if err != nil {
		return ""
	}

	s := doc.Find("body")
	doc.Find("h1,h2,h3,h4,h5,h6").Each(func(i int, header *goquery.Selection) {
		if d.classWeight(s) < 0 || d.getLinkDensity(s) > 0.33 {
			removeNodes(s)
		}
	})

	doc.Find("form,object,iframe,embed").Each(func(i int, s *goquery.Selection) {
		removeNodes(s)
	})

	if d.RemoveEmptyNodes {
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			html, _ := s.Html()
			if len(strings.TrimSpace(html)) == 0 {
				removeNodes(s)
			}
		})
	}

	d.cleanConditionally(s, "table,ul,div")

	// we'll sanitize all elements using a whitelist
	replaceWithWhitespace := map[string]bool{
		"br":         true,
		"hr":         true,
		"h1":         true,
		"h2":         true,
		"h3":         true,
		"h4":         true,
		"h5":         true,
		"h6":         true,
		"dl":         true,
		"dd":         true,
		"ol":         true,
		"li":         true,
		"ul":         true,
		"address":    true,
		"blockquote": true,
		"center":     true,
	}

	whitelist := make(map[string]bool)
	for _, tag := range d.WhitelistTags {
		tag = strings.ToLower(tag)
		whitelist[tag] = true
		delete(replaceWithWhitespace, tag)
	}

	var text string

	doc.Union(s.Find("*")).Each(func(i int, s *goquery.Selection) {
		if text != "" {
			return
		}

		// if element is in whitelist, delete all its attributes
		node := s.Get(0)
		if _, ok := whitelist[node.Data]; ok {
			node.Attr = make([]html.Attribute, 0)
		} else {
			// if root element, just use the text
			if s.Parent().Length() == 0 {
				text = s.Text()
				return
			} else {
				// replace node with children
				parent := node.Parent
				var next *html.Node

				for c := node.FirstChild; c != nil; c = next {
					next = c.NextSibling
					c.NextSibling = nil
					c.PrevSibling = nil
					c.Parent = nil

					parent.InsertBefore(c, node)
				}
			}
		}
	})

	if text == "" {
		text, _ = d.document.Html()
	}

	return normalizeWhitespaceRegexp.ReplaceAllString(text, "\n")
}

func (d *Document) cleanConditionally(s *goquery.Selection, selector string) {
	if !d.CleanConditionally {
		return
	}

	s.Find(selector).Each(func(i int, s *goquery.Selection) {
		node := s.Get(0)
		weight := float32(d.classWeight(s))
		contentScore := float32(0)

		if c, ok := d.candidates[node]; ok {
			contentScore = c.score
		}

		if weight+contentScore < 0 {
			removeNodes(s)
			log.Printf("Conditionally cleaned %s%s with weight %d and content score %d\n", node.Data, getName(s), weight, contentScore)
			return
		}

		text := s.Text()
		if strings.Count(text, ",") < 10 {
			counts := map[string]int{
				"p":     s.Find("p").Length(),
				"img":   s.Find("img").Length(),
				"li":    s.Find("li").Length() - 100,
				"a":     s.Find("a").Length(),
				"embed": s.Find("embed").Length(),
				"input": s.Find("input").Length(),
			}

			contentLength := len(strings.TrimSpace(text))
			linkDensity := d.getLinkDensity(s)
			remove := false
			reason := ""

			if counts["img"] > counts["p"] {
				reason = "too many images"
				remove = true
			} else if counts["li"] > counts["p"] && !s.Is("ul,ol") {
				reason = "more <li>s than <p>s"
				remove = true
			} else if counts["input"] > int(counts["p"]/3.0) {
				reason = "less than 3x <p>s than <input>s"
				remove = true
			} else if contentLength < d.MinTextLength && (counts["img"] == 0 || counts["img"] > 2) {
				reason = "too short content length without a single image"
				remove = true
			} else if weight < 25 && linkDensity > 0.2 {
				reason = fmt.Sprintf("too many links for its weight (%d)", weight)
				remove = true
			} else if weight >= 25 && linkDensity > 0.5 {
				reason = fmt.Sprintf("too many links for its weight (%d)", weight)
				remove = true
			} else if (counts["embed"] == 1 && contentLength < 75) || counts["embed"] > 1 {
				reason = "<embed>s with too short a content length, or too many <embed>s"
				remove = true
			}

			if remove {
				fmt.Printf("Conditionally cleaned %s with weight %d and content score %d because it has %s\n", getName(s), weight, contentScore, reason)
				removeNodes(s)
			}
		}
	})
}

func getName(s *goquery.Selection) string {
	class, _ := s.Attr("class")
	id, _ := s.Attr("id")

	return fmt.Sprintf("#%s.%s", id, class)
}

func traverse(node *html.Node, eval func(*html.Node)) {
	eval(node)

	// check each child
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		traverse(c, eval)
	}
}

func removeNodes(s *goquery.Selection) {
	s.Each(func(i int, s *goquery.Selection) {
		parent := s.Parent()
		if parent.Length() == 0 {
			// TODO???
		} else {
			parent.Get(0).RemoveChild(s.Get(0))
		}
	})
}
