package readability

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

var (
	Logger = log.New(ioutil.Discard, "[readability] ", log.LstdFlags)

	replaceBrsRegexp   = regexp.MustCompile(`(?i)(<br[^>]*>[ \n\r\t]*){2,}`)
	replaceFontsRegexp = regexp.MustCompile(`(?i)<(\/?)\s*font[^>]*?>`)

	blacklistCandidatesRegexp  = regexp.MustCompile(`(?i)popupbody`)
	okMaybeItsACandidateRegexp = regexp.MustCompile(`(?i)and|article|body|column|main|shadow`)
	unlikelyCandidatesRegexp   = regexp.MustCompile(`(?i)combx|comment|community|hidden|disqus|modal|extra|foot|header|menu|remark|rss|shoutbox|sidebar|sponsor|ad-break|agegate|pagination|pager|popup|share`)
	divToPElementsRegexp       = regexp.MustCompile(`(?i)<(dl|div|ol|pre|table|ul|header|footer|article)`)

	okMaybeItsAHeaderFooterRegexp = regexp.MustCompile(`(?i)(header|footer|h1|h2|h3|h4|h5|h6)`)

	negativeRegexp = regexp.MustCompile(`(?i)combx|comment|com-|foot|footer|footnote|masthead|media|meta|outbrain|promo|related|scroll|shoutbox|sidebar|sponsor|shopping|tags|tool|widget`)
	positiveRegexp = regexp.MustCompile(`(?i)article|body|content|entry|hentry|main|page|pagination|post|text|blog|story`)

	stripCommentRegexp = regexp.MustCompile(`(?s)\<\!\-{2}.+?-{2}\>`)

	sentenceRegexp = regexp.MustCompile(`\.( |$)`)

	normalizeWhitespaceRegexp     = regexp.MustCompile(`[\t ]+`)
	normalizeEOLRegexp            = regexp.MustCompile(`[\r\n\f]+`)
	normalizeHtmlWhiteSpaceRegexp = regexp.MustCompile(`(&nbsp;)+`)
)

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

	Title                    string
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
		WhitelistTags:            []string{"div", "p", "h1"},
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
	s = replaceFontsRegexp.ReplaceAllString(s, `<${1}span>`)

	// manually strip regexps since html parser seems to miss some
	s = stripCommentRegexp.ReplaceAllString(s, "")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return err
	}

	// if no body (like from a redirect or empty string)
	if doc.Find("body").Length() == 0 {
		s = "<body/>"
		return d.initializeHtml(s)
	}

	d.document = doc
	return nil
}

func (d *Document) Content() string {
	if d.content == "" {
		d.document.Find("html head").Children().Not("title").Each(func(i int, s *goquery.Selection) {
			removeNodes(s)
		})
		d.rateElements()
		content, _ := d.document.Html()
		sanitizeWhitespace(content)
		return content

		d.prepareCandidates()
		article := d.getArticle()
		articleText := d.sanitize(article)

		length := len(articleText)
		if length < d.RetryLength {
			retry := true

			if d.RemoveUnlikelyCandidates {
				d.RemoveUnlikelyCandidates = false
			} else if d.WeightClasses {
				d.WeightClasses = false
			} else if d.CleanConditionally {
				d.CleanConditionally = false
			} else {
				d.content = articleText
				retry = false
			}

			if retry {
				Logger.Printf("Retrying with length %d < retry length %d\n", length, d.RetryLength)
				d.initializeHtml(d.input)
				articleText = d.Content()
			}
		}
		d.content = articleText
	}

	return d.content
}

var (
	nonContentTags []string = []string{"meta", "script", "style", "iframe", "link", "form"}
)

func inArr(search string, arr []string) bool {
	for _, elem := range nonContentTags {
		if elem == search {
			return true
		}
	}
	return false
}

func elementNeedsRemoval(s *goquery.Selection) bool {
	return inArr(goquery.NodeName(s), nonContentTags)
}

var allowEmptyTag []string = []string{"hr", "br", "img"}

func validEmptyElement(s *goquery.Selection) bool {
	return inArr(goquery.NodeName(s), allowEmptyTag)
}

var (
	MinTextLength     int = 25
	ScoreValidContent int = 60
	ScoreIDMatch      int = 40
	ScoreClassMatch   int = 25
)

func getLinkDensity(s *goquery.Selection) float32 {
	linkLength := len(s.Find("a").Text())
	textLength := len(s.Text())

	if textLength == 0 {
		return 0
	}

	return float32(linkLength) / float32(textLength)
}

func evalSelection(s *goquery.Selection) int {
	if s.Length() > 1 {
		s.Children().Each(func(i int, ss *goquery.Selection) {
			evalSelection(ss)
		})
	}
	var score int

	Logger.Printf("Tag: %s", goquery.NodeName(s))
	Logger.Printf("Size: %d", s.Length())

	class, _ := s.Attr("class")
	id, _ := s.Attr("id")

	if len(class) > 0 {
		if negativeRegexp.MatchString(class) {
			score -= ScoreIDMatch
		}
		if positiveRegexp.MatchString(class) {
			score += ScoreIDMatch
		}
	}

	if len(id) > 0 {
		if negativeRegexp.MatchString(id) {
			score -= ScoreClassMatch
		}
		if positiveRegexp.MatchString(id) {
			score += ScoreClassMatch
		}
	}

	linkDensity := getLinkDensity(s)
	if linkDensity > 0.3 {
		s.Find("a").Each(func(i int, ss *goquery.Selection) {
			removeNodes(ss)
		})
	}

	content := sanitizeWhitespace(s.Text())
	contentLen := len(content)

	if contentLen == 0 {
		if validEmptyElement(s) {
			score += ScoreValidContent
		}
	}
	if contentLen > MinTextLength {
		score += contentLen
	}
	Logger.Printf("Content length: %d", len(content))
	Logger.Printf("Link density: %f", linkDensity)

	return score
}

func (d *Document) rateElements() {
	d.document.Find("body").Children().Each(func(i int, s *goquery.Selection) {
		if elementNeedsRemoval(s) {
			Logger.Printf("Removing non-content tag - %s[%d]\n", getName(s), i)
			removeNodes(s)
			return
		}
		Logger.Printf("Evaluating [%d::%d] - %s\n", i, s.Length(), getName(s))

		var score int
		score = evalSelection(s)

		Logger.Printf(" Score %d\n", score)
		if score < ScoreValidContent {
			Logger.Printf("Removing low score tag - %s[%d]\n", getName(s), i)
			removeNodes(s)
			return
		}
	})
}

func (d *Document) prepareCandidates() {
	d.document.Find(strings.Join(nonContentTags, ",")).Each(func(i int, s *goquery.Selection) {
		Logger.Printf("Removing no content tag - %s\n", getName(s))
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

func (d *Document) getTitle() string {
	return d.document.Find("head title").First().Text()
}

func (d *Document) getArticle() string {
	//output := bytes.NewBufferString("<div>")
	output := bytes.NewBufferString("")

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
				append = sentenceRegexp.MatchString(content)
			}
		}

		if append {
			tag := "div"
			if s.Is("p") {
				tag = n.Data
			}

			html, _ := s.Html()

			fmt.Fprintf(output, "<%s>%s</%s>\n", tag, html, tag)
		}
	})

	//output.Write([]byte("</div>"))

	return output.String()
}

func (d *Document) removeUnlikelyCandidates() {
	d.document.Find("*").Not("html,title,body").Each(func(i int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		id, _ := s.Attr("id")

		str := class + id

		if blacklistCandidatesRegexp.MatchString(str) || (unlikelyCandidatesRegexp.MatchString(str) && !okMaybeItsACandidateRegexp.MatchString(str) && !okMaybeItsAHeaderFooterRegexp.MatchString(goquery.NodeName(s))) {
			Logger.Printf("Removing unlikely candidate - %s\n", getName(s))
			removeNodes(s)
		}
	})
}

func (d *Document) transformMisusedDivsIntoParagraphs() {
	d.document.Find("header,footer,article,div").Each(func(i int, s *goquery.Selection) {
		html, err := s.Html()
		if err != nil {
			Logger.Printf("Unable to transform div to p %s\n", err)
			return
		}

		// transform <div>s that do not contain other block elements into <p>s
		if !divToPElementsRegexp.MatchString(html) {
			Logger.Printf("Altering %s to p\n", getName(s))

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
		if negativeRegexp.MatchString(class) && !okMaybeItsAHeaderFooterRegexp.MatchString(goquery.NodeName(s)) {
			weight -= 25
		}

		if positiveRegexp.MatchString(class) {
			weight += 25
		}
	}

	if id != "" {
		if negativeRegexp.MatchString(id) && !okMaybeItsAHeaderFooterRegexp.MatchString(goquery.NodeName(s)) {
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

var (
	headerTags []string = []string{"h1", "h2", "h3", "h4", "h5", "h6"}
)

func (d *Document) sanitize(article string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(article))
	d.Title = d.getTitle()
	if err != nil {
		Logger.Println("Unable to create document", err)
		return ""
	}

	s := doc.Find("body")
	s.Find(strings.Join(headerTags, ",")).Each(func(i int, header *goquery.Selection) {
		if d.classWeight(header) < 0 || d.getLinkDensity(header) > 0.33 {
			Logger.Printf("Removing tag - %s\n", getName(header))
			removeNodes(header)
		}
	})

	s.Find("input,select,textarea,button,object,iframe,embed").Each(func(i int, s *goquery.Selection) {
		Logger.Printf("Removing non-content tag - %s\n", getName(s))
		removeNodes(s)
	})

	if d.RemoveEmptyNodes {
		s.Find("p").Each(func(i int, s *goquery.Selection) {
			html, _ := s.Html()
			if len(strings.TrimSpace(html)) == 0 {
				Logger.Printf("Removing empty node - %s\n", getName(s))
				removeNodes(s)
			}
		})
	}

	d.cleanConditionally(s, "table,ul,div")

	// we'll sanitize all elements using a whitelist
	replaceWithWhitespace := map[string]bool{
		//"br":         true,
		//"hr":         true,
		//"h1":         true,
		//"h2":         true,
		//"h3":         true,
		//"h4":         true,
		//"h5":         true,
		//"h6":         true,
		"dl":      true,
		"dd":      true,
		"ol":      true,
		"li":      true,
		"ul":      true,
		"address": true,
		//"blockquote": true,
		"center": true,
	}

	whitelist := make(map[string]bool)
	for _, tag := range d.WhitelistTags {
		tag = strings.ToLower(tag)
		whitelist[tag] = true
		delete(replaceWithWhitespace, tag)
	}

	var text string

	s.Find("*").Each(func(i int, s *goquery.Selection) {
		// only look at element nodes
		node := s.Get(0)
		if node.Type != html.ElementNode {
			return
		}

		// if element is in whitelist, delete all its attributes
		if _, ok := whitelist[node.Data]; ok {
			node.Attr = make([]html.Attribute, 0)
		} else {
			if _, ok := replaceWithWhitespace[node.Data]; ok {
				// just replace with a text node and add whitespace
				node.Data = fmt.Sprintf(" %s ", s.Text())
				node.Type = html.TextNode
				node.FirstChild = nil
				node.LastChild = nil
			} else {
				if node.Parent == nil {
					text = s.Text()
					return
				} else {
					// replace node with children
					replaceNodeWithChildren(node)
				}
			}
		}
	})

	if text == "" {
		text, _ = doc.Html()
	}

	text = addTitle(d.Title, text)
	return sanitizeWhitespace(text)
}

func addTitle(title string, html string) string {
	var htmlTitle bytes.Buffer
	old := "<head></head>"

	htmlTitle.WriteString("<head><title>")
	htmlTitle.WriteString(title)
	htmlTitle.WriteString("</title></head>")

	return strings.Replace(html, old, htmlTitle.String(), 1)
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
			Logger.Printf("Conditionally cleaned %s%s with weight %f and content score %f\n", node.Data, getName(s), weight, contentScore)
			removeNodes(s)
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
				reason = fmt.Sprintf("too many links for its weight (%f)", weight)
				remove = true
			} else if weight >= 25 && linkDensity > 0.5 {
				reason = fmt.Sprintf("too many links for its weight (%f)", weight)
				remove = true
			} else if (counts["embed"] == 1 && contentLength < 75) || counts["embed"] > 1 {
				reason = "<embed>s with too short a content length, or too many <embed>s"
				remove = true
			}

			if remove {
				Logger.Printf("Conditionally cleaned %s with weight %f and content score %f because it has %s\n", getName(s), weight, contentScore, reason)
				removeNodes(s)
			}
		}
	})
}

func getName(s *goquery.Selection) string {
	class, _ := s.Attr("class")
	id, _ := s.Attr("id")
	idLbl, classLbl := "", ""
	if len(id) > 0 {
		idLbl = "#" + id
	}
	if len(class) > 0 {
		classLbl = "." + class
	}
	if s.Length() > 1 {
		return fmt.Sprintf("%s%s%s[%d]", goquery.NodeName(s), idLbl, classLbl, s.Length())
	} else {
		return fmt.Sprintf("%s%s%s", goquery.NodeName(s), idLbl, classLbl)
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

func replaceNodeWithChildren(n *html.Node) {
	var next *html.Node
	parent := n.Parent

	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		n.RemoveChild(c)

		parent.InsertBefore(c, n)
	}

	parent.RemoveChild(n)
}

func sanitizeWhitespace(text string) string {
	text = normalizeHtmlWhiteSpaceRegexp.ReplaceAllString(text, " ")
	text = normalizeWhitespaceRegexp.ReplaceAllString(text, " ")
	text = normalizeEOLRegexp.ReplaceAllString(text, "\n")
	text = strings.TrimSpace(text)
	return text
}
