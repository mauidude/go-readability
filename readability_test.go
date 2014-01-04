package readability

import (
	"io/ioutil"
	"strings"
	"testing"
)

type expectedOutput struct {
	requiredFragments []string
	excludedFragments []string
}

func TestGeneralFunctionality(t *testing.T) {
	html := `<html><head><title>title!</title></head><body><div><p>Some content</p></div></body>`
	doc, err := NewDocument(html)
	if err != nil {
		t.Fatal("Unable to create document", err)
	}

	doc.MinTextLength = 0
	doc.RetryLength = 1
	content := doc.Content()
	if !strings.Contains(content, "Some content") {
		t.Errorf("Expected content %q to match %q", content, "Some content")
	}
}

func TestIgnoringSidebars(t *testing.T) {
	html := `html><head><title>title!</title></head><body><div><p>Some content</p></div><div class='sidebar'><p>sidebar<p></div></body>`
	doc, err := NewDocument(html)
	if err != nil {
		t.Fatal("Unable to create document", err)
	}

	doc.MinTextLength = 0
	doc.RetryLength = 1

	content := doc.Content()
	if strings.Contains(content, "sidebar") {
		t.Errorf("Did not expect content %q to contain %q", content, "sidebar")
	}
}

func TestInsertSpaceForBlockElements(t *testing.T) {
	html := `<html><head><title>title!</title></head>
          <body>
            <div>
              <p>a<br>b<hr>c<address>d</address>f</p>
            </div>
          </body>
        </html>`

	doc, err := NewDocument(html)
	if err != nil {
		t.Fatal("Unable to create document", err)
	}

	content := doc.Content()
	if strings.Contains(content, "a b c d f") {
		t.Errorf("Did not expect content %q to contain %q", content, "a b c d f")
	}
}

func TestOutputForWellKnownDocuments(t *testing.T) {
	inputs := map[string]*expectedOutput{
		"blogpost_with_links.html": &expectedOutput{
			requiredFragments: []string{
				"The zebras and porcupines get together to beat the living shit out of zombies, who are trying to wreck the havoc upon them. The ceiling cat is awaken by the noise they're making, and summons the basement cat to do the punishment. Zombies bite the ceiling cat and ceiling cat decides to destroy the universe. Then the big bang happens and this shit doesn't matter anymore.",
				"Ceiling cat, the",
				"Basement cat, the",
			},
			excludedFragments: []string{},
		},

		"globemail-ottowa_cuts.html": &expectedOutput{
			requiredFragments: []string{
				"Treasury Board President Stockwell Day is trumpeting job cuts at government boards and agencies in the name of fiscal prudence – but the measures are largely phantom restraint because most affected posts are empty and have been for some time.",
				"Mr. Day, the Harper government's point man for belt-tightening in Ottawa, released Monday a list of 245 cabinet appointments that will be eliminated to make government more efficient.",
				"But 90 per cent of these positions are currently vacant. Many have been unfilled for years – and some for decades – often because the unused positions proved surplus to the needs of agencies or bodies.",
				`“This looks more token than real,” Kevin Gaudet, federal director of the fiscally conservative Canadian Taxpayers Federation, said of the cuts.`,
				"These reductions are the latest effort by the Conservative government, presiding over record deficits, to deliver two different messages at the same time. They want to show their political base they are keen on restraint but want to reassure Canadians in general that they are not slashing programs and services while the economic recovery is fragile.",
				"“For now, they seem content to nip and tuck where it doesn't really hurt. The real restraint will begin in 2011, after the recovery has more fully taken root,” BMO Nesbitt Burns deputy chief economist Douglas Porter said.",
				"At least 46 of the posts the Canadian government is eliminating have never been filled. These are jobs at boards and agencies that were created by past governments but never set up, including the Space Advisory Board, established in 1989 and eligible for 19 appointments by Ottawa.",
				"“No one has ever sat on that board,” said Erik Waddell, director of communications for Industry Minister Tony Clement.",
				"The same fate will befall 12 posts at the Canadian Council on the Status of the Artist – an organization created by a former Liberal government but never brought into being – and 15 positions at the Freshwater Fish Marketing Corporation's advisory committee, a panel that was never set up.",
				"Other cuts are not cost savings but changes in paperwork. For instance, a senior government surveyor will be appointed to multiple boundary commissions at once instead of separately. And two top bureaucrats will continue to serve as chair and vice-chair of a body that manages the Employment Insurance program – but they won't be cabinet appointments any more.",
				"Defending the measures, Mr. Day said the cuts matter even if the jobs are vacant now because it will prevent governments from ramping up the appointments down the road. “It's future savings,” he said.",
				"If all the jobs had been filled, Ottawa would be saving a paltry $1.2-million a year in pay and salaries as a result of these appointment cuts. By comparison, the budget deficit for the year ending March 31 will be $53.8-billion.",
				"But only 27 of the 245 jobs being cut are currently filled. A spokeswoman for Mr. Day said the estimated total savings from eliminating these 27 jobs is $53,000 to $62,000 in pay and $37,800 in travel bills.",
				"The Treasury Board President – responsible for scrutinizing spending – said the exercise may not save much but demonstrates Ottawa can operate more leanly.",
				"“It is what taxpayers want us to do, conduct the affairs of government and its services in an efficient way, and do it in a way that respects the taxpayers,” Mr. Day told the House of Commons.",
				"The Official Opposition Liberals, however, called the move to cut mostly vacant posts an empty and hypocritical gesture. They charge the Tories have used their patronage powers to appoint dozens of supporters to many of the very same boards and agencies in the past 16 months.",
				"“Since November 2008, 79 Conservatives who have donated $79,366.82 to the Conservative Party of Canada, Progressive Conservative Party, and the Canadian Alliance have been appointed to the same boards Minister Day singled out in his announcement today,” a release from Liberal critic Siobhan Coady said.",
			},
			excludedFragments: []string{
				"Share with friends",
				"Print or License",
				// a commenter
				"Hrmmm, rather pointless.",
				// another comment
				"Here's hoping the Conservatives react to the deficit",
				"<SCRIPT",
				"<script",
			},
		},
		"channel4-1.html": &expectedOutput{
			requiredFragments: []string{
				"Judge rules Briton can be force-fed",
				"A US judge has ruled that prison officials may continue force-feeding a Briton who began a hunger strike in September 2007 over claims he was convicted on a fabricated sexual assault charge.",
				"William Coleman, reportedly originally from Liverpool, who is serving an eight-year sentence for rape, said he began his hunger strike to protest against a corrupt judicial system.",
				"The state of Connecticut began force-feeding Coleman in September 2008 after he stopped accepting fluids, but he argued that the feedings violate his right of free speech.",
			},
			excludedFragments: []string{
				"Share this article",
			},
		},
		"foxnews-india1.html": &expectedOutput{
			requiredFragments: []string{
				"Police say 28 people have been killed in central India after the bus they were traveling in touched a high-voltage wire and caught fire.",
				"Police officer Ram Pyari Dhurwey says the accident occurred Friday in Mandla district in Madhya Pradesh state.",
				"It was the second such accident in India in as many days. At least 15 people were killed in eastern Bihar state on Thursday when they truck they were riding in touched a high-voltage wire.",
			},
			excludedFragments: []string{
				"Leave a comment",
				"Latest videos",
			},
		},
	}

	for file, expectedOutput := range inputs {
		bytes, err := ioutil.ReadFile("test_fixtures/" + file)
		if err != nil {
			t.Fatal("Unable to read file test_fixtures/", file, err)
		}

		input := string(bytes)
		doc, err := NewDocument(input)
		if err != nil {
			t.Fatal("Unable to create document", err)
		}

		content := doc.Content()
		content = normalizeString(content)

		for _, required := range expectedOutput.requiredFragments {
			required = normalizeString(required)
			if !strings.Contains(content, required) {
				t.Errorf("Expected content %q to contain %q", content, required)
			}
		}

		for _, excluded := range expectedOutput.excludedFragments {
			excluded = normalizeString(excluded)
			if strings.Contains(content, excluded) {
				t.Errorf("Did not expect content %q to contain %q", content, excluded)
			}
		}
	}
}

func normalizeString(s string) string {
	s = strings.Replace(s, "&#39;", "'", -1)

	return s
}
