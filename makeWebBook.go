// Copyright 2015 DLR-SR. All rights reserved.
// Use of this source code is governed by the
// Creative Commons Attribution-NonCommercial-ShareAlike 4.0 International License
// (http://creativecommons.org/licenses/by-nc-sa/4.0/).
/*
Author:
  Martin Otter, DLR-SR
  (http://www.robotic.dlr.de/sr/en/staff/martin.otter/)

Package makeWebBook updates HTML files to generate a web or local book
organized via several files. It is assumed that all files are present
in one directory, such as:

  /bookDirectory
    /resources             // directory
      /media               // directory of media files (e.g. images)
      /styles              // directory of style and javascript files
      configuration.json   // required file describing the book structure
    index.html             // cover file
    preface.html
    tableofcontents.html
    chapter_01.html       // section file 1 (defined in configuration.json)
    chapter_02.html       // section file 2
    chapter_03.html       // section file 3
    chapter_A.html        // appendix
    references.html       // references

With the command

  makeWebBook bookDirectory

the actions described below are performed, provided a corresponding
<h1> element starts with the text "Chapter" or "Appendix".
(otherwise the <h1> section is not modified; this is useful for a
preface or a literature chapter)

- Specific html elements get a number. In particular:
    <h1>, <h2>, <h3>, <h4> elements are updated with section numbers
      Examples:
        <h1>: Chapter 3 - Operators and Expressions
              Appendix B - Concrete Syntax
        <h2>: 3.2 Array Operators
        <h3>: 3.2.3 Array Multiplication
        <h4>: 3.2.3.5 Matrix Multiplication

    <caption> elements are updated with a caption number, e.g.
       "Table 3-4: This is a table"

    <figcaption> elements are updated with a figcaption number, e.g.
       "Figure 3-7: This is a figure"

    Equations marked by
           <div class="equation"> $$  ...  $$ </div>
       are updated with an equation number (note, it is important that
       exactly the string `<div class="equation"` is used with exactly one
       space between "div" and "class"). Example:
           <div class="equation"> $$ (2.1) \;\;\; ax^2 + bx + c = 0$$ </div>

  If a number is not present, it is introduced (with exception of <h1>
  element, where a number is only introduced if the text starts with
  "Chapter" or with "Appendix").
  If it is present and correct, nothing is changed.
  Otherwise, the number is updated.

- A navigation bar is introduced in all files with links to the
  "table of contents" file, the previous, and the next file.

- The "table of contents" file is updated with the actual document
  structure. The "table of contents" file must be defined by the user.
  The text within the html comment
     <!-- BeginTableOfContents -->
        ...
     <!-- EndTableOfContents -->
  is removed and replaced by the actual document structure

- If a section file needs no update, it is not changed.
  If a file is changed, it is first moved in a backup directory
  (defined in the configuration.json file), and then the file
  is newly generated with the updated information.
*/
package main

import (
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	// "net/http"
)

type ConfigurationType struct {
	BackupDirectory   string   `json:"BackupDirectory"`
	CoverFileName     string   `json:"CoverFileName"`
	TocFileName       string   `json:"TableOfContentsFileName"`
	SectionsFileNames []string `json:"SectionsFileNames"`
}

// Structure of one book section (h1, h2, ...), used to generate the "table of contents"
type SectionType struct {
	FileName  string         // File where section is present
	ID        string         // <hx id=ID>
	Text      string         // <hx id=ID>Text</hx>
	Modified  bool           // = true, if Text was modified (section/caption/equation number); = false, if it was not modified
	Sections  []SectionType  // subsections in this section
	Captions  []CaptionType  // captions and figcaptions in this section before any of the subsections
	Equations []EquationType // equations in this section before any of the subsections
}

// Table "caption" or figure "figcaption" information
type CaptionType struct {
	FileName   string
	ID         string // <caption id=ID> or <figcaption id=ID>
	Text       string // <caption id=ID>Text</caption> or <figcaption id=ID>Text</figcaption>
	Modified   bool   // = true, if Text was modified (section/caption number); = false, if it was not modified
	Figcaption bool   // = true, if figcaption, otherwise caption
}

// Equation information
type EquationType struct {
	FileName string
	ID       string // <div class="equation" id=ID>
	Text     string // <div class="equation" id=ID>Text</div>
	Modified bool   // = true, if Text was modified; = false, if it was not modified
}

// Information of one found element, used to update the file
type ElementType struct {
	StartTag string // Start-tag of element, without closing ">" and without attributes (e.g. "<h1")
	EndTag   string // End-tag of element (e.g. "</h1>")
	Text     string // Text of element
	Href     string // If StartTag == "<a" then (if Href != "" then internal link: <a href="Href">..</a> else external link)
	// else Href="" (dummy)
	NewText string // If Modified = true, the modified text, otherwise Text
	// (If StartTag=="<a" then target file name: <a href="TargetFileName#TargetID" title="Tooltip">Text</a>)
	Tooltip  string // If StartTag == "<a then tooltip; otherwise Tooltip="" (dummy)
	Modified bool   // = true, if Text was modified (e.g. section or caption number)
	ID       string // id attribute of element
	// or targetID if startTag = "<a"
	NewID bool // = true, if a new ID was generated, because no ID was present
}

// Information about the modified data on a file
type SectionFileType struct {
	FileName  string
	NewNav    bool // = true, if no nav was present in the file and a new one needs to be generated
	UpdateNav bool // If NewNav = false (otherwise dummy):
	Modified  bool // = true, if at least one element in Elements needs to be modified
	Elements  []ElementType
}

// Information about a bookmark. All bookmarks are collected
// in a map where the "id" attribute is used as key
//    see section <a href="chapter_02.html#sec_operators>2.3.1</a>
// Key     : "sec_operators"
// FileName: "chapter_02.html"
// Ref     : "2.3.1"
type BookmarkType struct {
	FileName string // File name of bookmark
	Label    string // Reference label, such as "Chapter 2", "2.3", "Figure 3-2"
	Tooltip  string // Text to be used as tooltip
}

/*
// Information about a Link
type LinkType struct {
   FileName       string  // File in which link is present
   TargetFileName string  // Target file name
   TargetID       string  // Target ID
   Label          string  // Link text (should be a label like "Chapter 2" or "2.3"
}
*/

// Complete book structure
type BookStructureType struct {
	CoverFileName string
	TocFileName   string
	SectionFiles  []SectionFileType // Files in which the sections are present
	Sections      []SectionType     // h1 sections
}

// Counters
type CountersType struct {
	iFigCaption  int
	iCaption     int
	iEquation    int
	ih1_digit    int
	ih1_letter   int
	last_h1_type string // = "Chapter" or "Appendix" or ""
}

// Global variable holding the complete structure of the document
var Configuration ConfigurationType
var BookStructure BookStructureType
var Bookmarks = make(map[string]BookmarkType)

// Global variable holding the full path to the actual backup directory
var BackupPath string

// Global variable holding all counters
var Counters CountersType

// Compiled regular expressions as global variables
var validSection1 = regexp.MustCompile(`^Chapter [1-9][0-9]* `)                                      // e.g. "Chapter 4 "
var validSection2 = regexp.MustCompile(`^[1-9][0-9]*[.][1-9][0-9]* `)                                // e.g. "4.2 "
var validSection3 = regexp.MustCompile(`^[1-9][0-9]*[.][1-9][0-9]*[.][1-9][0-9]* `)                  // e.g. "4.2.3 "
var validSection4 = regexp.MustCompile(`^[1-9][0-9]*[.][1-9][0-9]*[.][1-9][0-9]*[.][1-9][0-9]* `)    // e.g. "4.2.3.5 "
var validSection1_Appendix = regexp.MustCompile(`^Appendix [A-Z] `)                                  // e.g. "Appendix B "
var validSection2_Appendix = regexp.MustCompile(`^[A-Z][.][1-9][0-9]* `)                             // e.g. "B.2 "
var validSection3_Appendix = regexp.MustCompile(`^[A-Z][.][1-9][0-9]*[.][1-9][0-9]* `)               // e.g. "B.2.3 "
var validSection4_Appendix = regexp.MustCompile(`^[A-Z][.][1-9][0-9]*[.][1-9][0-9]*[.][1-9][0-9]* `) // e.g. "B.2.3.5 "
var validCaption = regexp.MustCompile(`^Table [1-9][0-9]*[-][1-9][0-9]*: `)                          // e.g. "Table 3-2: "
var validFigCaption = regexp.MustCompile(`^Figure [1-9][0-9]*[-][1-9][0-9]*: `)                      // e.g. "Figure 3-2: "
var validCaption_Appendix = regexp.MustCompile(`^Table [A-Z][-][1-9][0-9]*: `)                       // e.g. "Table B-2: "
var validFigCaption_Appendix = regexp.MustCompile(`^Figure [A-Z][-][1-9][0-9]*: `)                   // e.g. "Figure B-2: "
var validEquation = regexp.MustCompile(`\s*[$][$]\s*[(][1-9][0-9]*[.][1-9][0-9]*[)]`)                // e.g. "$$ (2.3)"
var validEquation_Appendix = regexp.MustCompile(`\s*[$][$]\s*[(][A-Z][.][1-9][0-9]*[)]`)             // e.g. "$$ (B.3)"
var withEquationNumber = regexp.MustCompile(`\s*[$][$]\s*[(]`)                                       // e.g. "$$ ("
var equationStart = regexp.MustCompile(`\s*[$][$]`)                                                  // e.g. "$$"

// Constants
const beginTableOfContents = "<!-- BeginTableOfContents -->"
const endTableOfContents = "<!-- EndTableOfContents -->"
const beginNavBar = "<nav>"
const endNavBar = "</nav>"
const beginBody = "<body>"
const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const maxDisplayCharacters = 40 // Maximum number of characters to be showed for captions in Table-of-Contents

func main() {
	// One input argument required: Directory in which book files are present
	// Configuration file must be here: "<arg>/resources/configuration.json"
	nArgs := len(os.Args)
	if nArgs < 2 {
		fmt.Println("Error: No directory name given as input argument for makeWebBook.exe")
		os.Exit(1)
	} else if nArgs > 2 {
		fmt.Println("Error: 2 or more arguments given to makeWebBook.exe, but only one argument is allowed")
	}
	bookDirectory := os.Args[1]

	// Change directory to the place where the configuration file is present
	err := os.Chdir(bookDirectory)
	if err != nil {
		log.Fatal(err)
	}
	bookPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("... Book directory that shall be processed:", bookPath)

	// Read configuration file
	fullConfigurationFileName := filepath.Join(bookPath, "resources", "configuration.json")
	fmt.Println("Configuration file:", fullConfigurationFileName)
	getConfiguration(fullConfigurationFileName)

	// Generate and log backup directory
	BackupPath = makeBackupDirectory(Configuration.BackupDirectory)

	// Get document structure (store in global variable BookStructure)
	getDocumentStructure()

	// Update section documents (changed section or caption numbers, introducing ids, etc.)
	updateSectionDocuments()

	// Generate Table-of-Contents file
	movedContentsFileName := filepath.Join(BackupPath, BookStructure.TocFileName)
	err = os.Rename(BookStructure.TocFileName, movedContentsFileName)
	if os.IsNotExist(err) {
		// No contents file exists; generate a new one
		writeContentsFile("", BookStructure.TocFileName)
	} else if err != nil {
		log.Fatal(err)
	} else {
		// BookStructure file exists and was moved
		writeContentsFile(movedContentsFileName, BookStructure.TocFileName)
	}
}

// Get actual time as string so that the string can be used as directory name (":" is replaced by "-")
func getActualTimeAsString() string {
	actualTime := time.Now()
	str1 := actualTime.Format(time.RFC3339)
	str2 := strings.Replace(str1, ":", "-", -1)
	return str2
}

// Make backup directory: input: directory to place backup directory; output: full path name of backup directory
func makeBackupDirectory(directoryName string) string {
	if os.Mkdir(directoryName, 0700) != nil {
		// Mkdir failed: Check that the existing file is a directory
		fileInfo, err := os.Stat(directoryName)
		if err != nil {
			log.Fatal(err)
		}
		if !fileInfo.IsDir() {
			log.Fatalf("Backup directory name \"%s\" is not a directory\n", directoryName)
		}
	}
	actualTime := getActualTimeAsString()
	workingDirectory, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	err = os.Chdir(directoryName)
	if err != nil {
		log.Fatal(err)
	}
	err = os.Mkdir(actualTime, 0700)
	if err != nil {
		log.Fatal(err)
	}
	err = os.Chdir(actualTime)
	if err != nil {
		log.Fatal(err)
	}
	backupPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	err = os.Chdir(workingDirectory)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Backup directory:", backupPath)
	return backupPath
}

func getConfiguration(fileName string) {
	raw, err := ioutil.ReadFile(fileName)
	if err != nil {
		fmt.Println("... Could not read configuration file:", err.Error())
		os.Exit(1)
	}

	err = json.Unmarshal(raw, &Configuration)
	if err != nil {
		fmt.Println("... Error in json configuration file \"", fileName, "\": ", err.Error())
		os.Exit(2)
	}
	return
}

// Determine document structure and store results in gobal variable BookStructure
func getDocumentStructure() {
	fmt.Println("Determine document structure:")
	BookStructure = BookStructureType{
		CoverFileName: Configuration.CoverFileName,
		TocFileName:   Configuration.TocFileName,
		SectionFiles:  make([]SectionFileType, 0, 10),
		Sections:      make([]SectionType, 0, 10)}

	// Initialize new random number generator (in order to generator random id's, if no ones are present)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Determine structure of every section file
	for iFile, file := range Configuration.SectionsFileNames {
		getStructureOfOneFile(file, iFile, r)
	}
}

func getStructureOfOneFile(fileName string, iFile int, r *rand.Rand) {
	fmt.Println("  ", fileName)

	// Store file name and default section/caption structure
	BookStructure.SectionFiles = append(BookStructure.SectionFiles,
		SectionFileType{fileName, true, false, false, make([]ElementType, 0, 10)})
	iSectionFile := len(BookStructure.SectionFiles) - 1

	// Open file
	file, err1 := os.Open(fileName)
	if err1 != nil {
		log.Fatal(err1)
	}
	defer file.Close()

	// Query section structure present in file
	doc, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		log.Fatal(err)
	}

	element := false
	iNav := 0

	doc.Find("h1,h2,h3,h4,caption,figcaption,a,nav,div.equation,ul.references").Each(func(i int, s *goquery.Selection) {
		// Inquire whether nav element is present
		if s.Is("nav") {
			// Check that nav is before any other element
			if element {
				fmt.Println("Error: <nav> present after a section/caption/figcaption element on file:", fileName)
				fmt.Println("       This is not supported.")
				os.Exit(1)
			}

			// Mark that navigation bar is already present in file.
			BookStructure.SectionFiles[iSectionFile].NewNav = false

			// Inquire file references in navigation bar
			var navFiles [3]string
			var navRequiredFiles [3]string
			navFiles[0] = ""
			navFiles[1] = ""
			navFiles[2] = ""

			s.Find("a").Each(func(i int, ss *goquery.Selection) {
				if i > 2 {
					fmt.Println("Error: Existing <nav> has more as 3 <a> elements in file: ", fileName)
					fmt.Println("       This is not supported")
					os.Exit(1)
				}
				navFiles[i] = ss.AttrOr("href", "???")
				iNav++
			})

			// Check whether the three file references are up-to-date
			navRequiredFiles[0] = Configuration.TocFileName
			if iSectionFile > 0 {
				navRequiredFiles[1] = Configuration.SectionsFileNames[iSectionFile-1]
			} else {
				navRequiredFiles[1] = Configuration.CoverFileName
			}
			if iSectionFile < len(Configuration.SectionsFileNames)-1 {
				navRequiredFiles[2] = Configuration.SectionsFileNames[iSectionFile+1]
			} else {
				navRequiredFiles[2] = ""
			}

			if navFiles[0] != navRequiredFiles[0] ||
				navFiles[1] != navRequiredFiles[1] ||
				navFiles[2] != navRequiredFiles[2] {
				BookStructure.SectionFiles[iSectionFile].UpdateNav = true
			}
			return
		} else {
			element = true
		}

		if s.Is("a") { // Link detected
			// Check if link is pointing into the book
			if iNav > 0 {
				// Link from the navigation bar (ignore it)
				iNav--
				return
			}
			href, exists := s.Attr("href")
			if !exists {
				fmt.Printf("Warning: link <a> without href attribute is ignored in file %s\n", fileName)
				BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
					ElementType{"<a", "</a>", "", "", "", "", false, "", false})
				return
			}
			if strings.Index(href, "/") == -1 {
				// No "/", so link internal to the book
				var targetFileName string
				var targetID string
				tooltip := s.AttrOr("title", "")

				IDstart := strings.Index(href, "#")
				if IDstart == -1 {
					// No "#"
					targetFileName = href
					targetID = ""
				} else if IDstart == 0 {
					// "#xxx", so no file name
					if len(href) <= 1 {
						fmt.Printf("Error: Wrong link '<a href=\"#\">' in file %s\n", fileName)
						os.Exit(1)
					}
					targetFileName = fileName
					targetID = href[IDstart+1:]
				} else {
					// "xxx#yyy"
					if IDstart+1 >= len(href) {
						targetFileName = href[0:IDstart]
						targetID = ""
					} else {
						targetFileName = href[0:IDstart]
						targetID = href[IDstart+1:]
					}
				}
				BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
					ElementType{"<a", "</a>", s.Text(), href, targetFileName, tooltip, false, targetID, false})

			} else {
				BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
					ElementType{"<a", "</a>", "", "", "", "", false, "", false})
				/*
				   // External link, check whether it exists
				   _, err := http.Get(href);
				   if err != nil {
				   fmt.Printf("Error when opening link %s\n", href)
				*/
			}
			return
		}

		// Store id's of references
		if s.Is("ul.references") { // references detected
			s.Find("li").Each(func(i int, s2 *goquery.Selection) {
				id, exists := s2.Attr("id")
				if !exists || id == "" || id == "#" {
					// No id is present, ignore this list item
					return
				} else {
					// Find text between <strong> ... </strong>
					tooltip := ""
					s2.Find("strong").Each(func(i int, s3 *goquery.Selection) {
						tooltip = s3.Text()
					})

					// Store id as bookmark
					title, exists := s2.Attr("title")
					if exists && title != "" {
						addBookmark(id, fileName, title, tooltip)
					} else {
						addBookmark(id, fileName, "", tooltip)
					}
				}
			})
			return
		}

		// Inquire element id and content (= text + label)
		var label string
		newID := false
		id, exists := s.Attr("id")
		if !exists || id == "" || id == "#" {
			// If no id present, introduce a random value for id
			id = strconv.Itoa(int(r.Int31()))
			newID = true
		}
		text := s.Text()
		modified := false // = true, if text is modified
		var newText string

		// Actual index of SectionFiles
		iFile := len(BookStructure.SectionFiles) - 1

		// Store information
		if s.Is("h1") {
			Counters.iFigCaption = 0
			Counters.iCaption = 0
			Counters.iEquation = 0

			// Determine chapter number
			isec := minInt(len("Chapter"), len(text))
			if text[0:isec] == "Chapter" {
				// Increment chapter number
				Counters.ih1_digit++
				Counters.last_h1_type = "Chapter"
			} else {
				isec = minInt(len("Appendix"), len(text))
				if text[0:isec] == "Appendix" {
					// Increment appendix number
					Counters.ih1_letter++
					Counters.last_h1_type = "Appendix"
				} else {
					Counters.last_h1_type = ""
				}
			}

			// Update h1 section number if necessary and make a new h1 entry in BookStructure
			newText, modified, label = updateSectionText(text, 1, 0, 0, 0)
			BookStructure.Sections = append(BookStructure.Sections,
				SectionType{fileName, id, newText, modified,
					make([]SectionType, 0, 5),
					make([]CaptionType, 0, 5),
					make([]EquationType, 0, 5)})
			BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
				ElementType{"<h1", "</h1>", text, "", newText, "", modified, id, newID})

		} else if s.Is("h2") {
			i1 := len(BookStructure.Sections) - 1
			if i1 < 0 {
				fmt.Println("h2 defined before h1 in file:", fileName)
				os.Exit(1)
			}
			i2 := len(BookStructure.Sections[i1].Sections)
			newText, modified, label = updateSectionText(text, 2, i2+1, 0, 0)
			BookStructure.Sections[i1].Sections =
				append(BookStructure.Sections[i1].Sections,
					SectionType{fileName, id, newText, modified,
						make([]SectionType, 0, 5),
						make([]CaptionType, 0, 5),
						make([]EquationType, 0, 5)})
			BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
				ElementType{"<h2", "</h2>", text, "", newText, "", modified, id, newID})

		} else if s.Is("h3") {
			i1 := len(BookStructure.Sections) - 1
			if i1 < 0 {
				fmt.Println("h2 defined before h1 in file:", fileName)
				os.Exit(1)
			}
			i2 := len(BookStructure.Sections[i1].Sections) - 1
			if i2 < 0 {
				fmt.Println("h3 defined before h2 in file:", fileName)
				os.Exit(1)
			}
			i3 := len(BookStructure.Sections[i1].Sections[i2].Sections)
			newText, modified, label = updateSectionText(text, 3, i2+1, i3+1, 0)
			BookStructure.Sections[i1].Sections[i2].Sections =
				append(BookStructure.Sections[i1].Sections[i2].Sections,
					SectionType{fileName, id, newText, modified,
						make([]SectionType, 0, 5),
						make([]CaptionType, 0, 5),
						make([]EquationType, 0, 5)})
			BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
				ElementType{"<h3", "</h3>", text, "", newText, "", modified, id, newID})

		} else if s.Is("h4") {
			i1 := len(BookStructure.Sections) - 1
			if i1 < 0 {
				fmt.Println("h2 defined before h1 in file:", fileName)
				os.Exit(1)
			}
			i2 := len(BookStructure.Sections[i1].Sections) - 1
			if i2 < 0 {
				fmt.Println("h3 defined before h2 in file:", fileName)
				os.Exit(1)
			}
			i3 := len(BookStructure.Sections[i1].Sections[i2].Sections) - 1
			if i3 < 0 {
				fmt.Println("h4 defined before h3 in file:", fileName)
				os.Exit(1)
			}
			i4 := len(BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections)
			newText, modified, label = updateSectionText(text, 4, i2+1, i3+1, i4+1)
			BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections =
				append(BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections,
					SectionType{fileName, id, newText, modified,
						make([]SectionType, 0, 1),
						make([]CaptionType, 0, 1),
						make([]EquationType, 0, 5)})
			BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
				ElementType{"<h4", "</h4>", text, "", newText, "", modified, id, newID})

		} else if s.Is("caption") || s.Is("figcaption") {
			var fig bool
			var iCap int
			if s.Is("caption") {
				fig = false
				Counters.iCaption++
				iCap = Counters.iCaption
			} else {
				fig = true
				Counters.iFigCaption++
				iCap = Counters.iFigCaption
			}

			i1 := len(BookStructure.Sections) - 1
			if i1 < 0 {
				fmt.Printf("caption/figcaption in file \"%s\" defined before first h1 defined in book", fileName)
				os.Exit(1)
			}

			newText, modified, label = updateCaptionText(text, fig, iCap)
			i2 := len(BookStructure.Sections[i1].Sections) - 1
			if i2 < 0 {
				BookStructure.Sections[i1].Captions =
					append(BookStructure.Sections[i1].Captions, CaptionType{fileName, id, newText, modified, fig})
			} else {
				i3 := len(BookStructure.Sections[i1].Sections[i2].Sections) - 1
				if i3 < 0 {
					BookStructure.Sections[i1].Sections[i2].Captions =
						append(BookStructure.Sections[i1].Sections[i2].Captions,
							CaptionType{fileName, id, newText, modified, fig})
				} else {
					i4 := len(BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections) - 1
					if i4 < 0 {
						BookStructure.Sections[i1].Sections[i2].Sections[i3].Captions =
							append(BookStructure.Sections[i1].Sections[i2].Sections[i3].Captions,
								CaptionType{fileName, id, newText, modified, fig})
					} else {
						BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections[i4].Captions =
							append(BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections[i4].Captions,
								CaptionType{fileName, id, newText, modified, fig})
					}
				}
			}
			if fig {
				BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
					ElementType{"<figcaption", "</figcaption>", text, "", newText, "", modified, id, newID})
			} else {
				BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
					ElementType{"<caption", "</caption>", text, "", newText, "", modified, id, newID})
			}

		} else if s.Is("div.equation") {
			Counters.iEquation++

			i1 := len(BookStructure.Sections) - 1
			if i1 < 0 {
				fmt.Printf("<div class=\"equation\"> in file \"%s\" defined before first h1 defined in book", fileName)
				os.Exit(1)
			}

			newText, modified, label = updateEquationText(text)
			i2 := len(BookStructure.Sections[i1].Sections) - 1
			if i2 < 0 {
				BookStructure.Sections[i1].Equations =
					append(BookStructure.Sections[i1].Equations, EquationType{fileName, id, newText, modified})
			} else {
				i3 := len(BookStructure.Sections[i1].Sections[i2].Sections) - 1
				if i3 < 0 {
					BookStructure.Sections[i1].Sections[i2].Equations =
						append(BookStructure.Sections[i1].Sections[i2].Equations,
							EquationType{fileName, id, newText, modified})
				} else {
					i4 := len(BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections) - 1
					if i4 < 0 {
						BookStructure.Sections[i1].Sections[i2].Sections[i3].Equations =
							append(BookStructure.Sections[i1].Sections[i2].Sections[i3].Equations,
								EquationType{fileName, id, newText, modified})
					} else {
						BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections[i4].Equations =
							append(BookStructure.Sections[i1].Sections[i2].Sections[i3].Sections[i4].Equations,
								EquationType{fileName, id, newText, modified})
					}
				}
			}
			BookStructure.SectionFiles[iFile].Elements = append(BookStructure.SectionFiles[iFile].Elements,
				ElementType{"<div class=\"equation\"", "</div>", text, "", newText, "", modified, id, newID})
		}

		if modified || newID {
			BookStructure.SectionFiles[iFile].Modified = true
		}

		if newID {
			// Print information about introduced ID
			iElem := len(BookStructure.SectionFiles[iFile].Elements) - 1
			elem := BookStructure.SectionFiles[iFile].Elements[iElem]
			fmt.Printf("      Element id introduced: %s id=\"%s\">%s%s\n",
				elem.StartTag, id, newText, elem.EndTag)
		}

		// Store bookmark
		if s.Is("div.equation") {
			addBookmark(id, fileName, label, "") // no tool tip for a link to an equation
		} else {
			addBookmark(id, fileName, label, newText)
		}
	})
}

func addBookmark(id string, fileName string, label string, tooltip string) {
	key, present := Bookmarks[id]
	if present {
		fmt.Printf("ERROR: Bookmark with id = \"%s\" present twice:\n", id)
		fmt.Printf("       First  location: FileName = \"%s\", Label = \"%s\", Tooltip =\"%s\"\n", key.FileName, key.Label, key.Tooltip)
		fmt.Printf("       Second location: FileName = \"%s\", Label = \"%s\", Tooltip =\"%s\"\n", fileName, label, tooltip)
	} else {
		Bookmarks[id] = BookmarkType{fileName, label, tooltip}
	}
}

// Integer minimum
func minInt(a, b int) int {
	if a <= b {
		return a
	} else {
		return b
	}
}

// Update text with correct section number
func updateSectionText(text string, level, nr2, nr3, nr4 int) (newText string, modified bool, label string) {
	// If section needs not to be numbered, return
	if Counters.last_h1_type == "" {
		newText = text
		modified = false
		label = text
		return
	}

	// Section number needs to be numbered
	var secStr string // Required section number as string

	// Determine required section number
	if Counters.last_h1_type == "Chapter" {
		switch level {
		case 1:
			secStr = fmt.Sprintf("Chapter %d ", Counters.ih1_digit)
		case 2:
			secStr = fmt.Sprintf("%d.%d ", Counters.ih1_digit, nr2)
		case 3:
			secStr = fmt.Sprintf("%d.%d.%d ", Counters.ih1_digit, nr2, nr3)
		case 4:
			secStr = fmt.Sprintf("%d.%d.%d.%d ", Counters.ih1_digit, nr2, nr3, nr4)
		default:
			fmt.Printf("Wrong argument level (= %d) when calling function updateText.\nMust be 1,2,3 or 4\n", level)
			os.Exit(1)
		}
	} else {
		h1_letter := string(letters[Counters.ih1_letter-1])
		switch level {
		case 1:
			secStr = fmt.Sprintf("Appendix %s ", h1_letter)
		case 2:
			secStr = fmt.Sprintf("%s.%d ", h1_letter, nr2)
		case 3:
			secStr = fmt.Sprintf("%s.%d.%d ", h1_letter, nr2, nr3)
		case 4:
			secStr = fmt.Sprintf("%s.%d.%d.%d ", h1_letter, nr2, nr3, nr4)
		default:
			fmt.Printf("Wrong argument level (= %d) when calling function updateText.\nMust be 1,2,3 or 4\n", level)
			os.Exit(1)
		}
	}
	label = secStr[0 : len(secStr)-1]

	// Has text the required section number?
	isec := minInt(len(secStr), len(text))
	if text[0:isec] == secStr {
		// text has the required section number
		newText = text
		modified = false

	} else {
		// text has no or wrong section number -> correct section number
		var index []int
		byteText := []byte(text)

		if Counters.last_h1_type == "Chapter" {
			switch level {
			case 1:
				index = validSection1.FindIndex(byteText)
			case 2:
				index = validSection2.FindIndex(byteText)
			case 3:
				index = validSection3.FindIndex(byteText)
			case 4:
				index = validSection4.FindIndex(byteText)
			}
		} else {
			switch level {
			case 1:
				index = validSection1_Appendix.FindIndex(byteText)
			case 2:
				index = validSection2_Appendix.FindIndex(byteText)
			case 3:
				index = validSection3_Appendix.FindIndex(byteText)
			case 4:
				index = validSection4_Appendix.FindIndex(byteText)
			}
		}

		if index == nil {
			// no Section number was present
			newText = secStr + text
			fmt.Println("      Section number added:", newText)
		} else {
			// Section number was present: replace it with correct one
			newText = secStr + string(byteText[index[1]:])
			fmt.Println("      Section number updated:", newText)
		}
		modified = true
	}
	return
}

// Update text with correct caption number
func updateCaptionText(text string, fig bool, nrCap int) (newText string, modified bool, label string) {
	// If caption needs not to be numbered, return
	if Counters.last_h1_type == "" {
		newText = text
		modified = false
		label = text
		return
	}

	// Caption number needs to be numbered
	var capStr string // Required caption number as string

	// Determine required caption number
	if Counters.last_h1_type == "Chapter" {
		if fig {
			capStr = fmt.Sprintf("Figure %d-%d: ", Counters.ih1_digit, nrCap)
		} else {
			capStr = fmt.Sprintf("Table %d-%d: ", Counters.ih1_digit, nrCap)
		}
	} else {
		h1_letter := string(letters[Counters.ih1_letter-1])
		if fig {
			capStr = fmt.Sprintf("Figure %s-%d: ", h1_letter, nrCap)
		} else {
			capStr = fmt.Sprintf("Table %s-%d: ", h1_letter, nrCap)
		}
	}
	label = capStr[0 : len(capStr)-2]

	// Has text the required caption number?
	icap := minInt(len(capStr), len(text))
	if text[0:icap] == capStr {
		// text has the required caption number
		newText = text
		modified = false

	} else {
		// text has no or wrong caption number -> correct caption number
		var index []int
		byteText := []byte(text)

		if Counters.last_h1_type == "Chapter" {
			if fig {
				index = validFigCaption.FindIndex(byteText)
			} else {
				index = validCaption.FindIndex(byteText)
			}
		} else {
			if fig {
				index = validFigCaption_Appendix.FindIndex(byteText)
			} else {
				index = validCaption_Appendix.FindIndex(byteText)
			}
		}

		if index == nil {
			// no caption number was present
			newText = capStr + text
			fmt.Println("      Caption number added:", newText)
		} else {
			// Caption number was present: replace it with correct one
			newText = capStr + string(byteText[index[1]:])
			fmt.Println("      Caption number updated:", newText)
		}
		modified = true
	}
	return
}

// Update text with correct equation number
func updateEquationText(text string) (newText string, modified bool, label string) {
	// If section needs not to be numbered, return
	if Counters.last_h1_type == "" {
		newText = text
		modified = false
		label = ""
		return
	}

	// Equation number needs to be numbered
	var eqStr string // Required equation number as string

	// Determine required equation number
	if Counters.last_h1_type == "Chapter" {
		eqStr = fmt.Sprintf("(%d.%d)", Counters.ih1_digit, Counters.iEquation)
	} else {
		h1_letter := string(letters[Counters.ih1_letter-1])
		eqStr = fmt.Sprintf("(%s.%d)", h1_letter, Counters.iEquation)
	}
	label = eqStr

	// Has text the required equation number?
	byteText := []byte(text)
	var index []int
	if Counters.last_h1_type == "Chapter" {
		index = validEquation.FindIndex(byteText)
	} else {
		index = validEquation_Appendix.FindIndex(byteText)
	}

	if index == nil {
		// No valid equation number present, add a new one
		index = equationStart.FindIndex(byteText) // find "$$"
		if index == nil {
			fmt.Printf("Error: <div class=\"equation\" ...> present, but no \"$$\" to mark equation start\n")
			os.Exit(1)
		}
		newText = text[0:index[1]] + " " + eqStr + ` \;\;\;\;\; ` + text[index[1]:]
		fmt.Println("      Equation number added:", newText)
      modified = true
	} else {
		// Check whether equation number is correct
		iEnd := index[1]
		index = withEquationNumber.FindIndex(byteText) // find "$$ ("
		iBegin := index[1] - 1
		if text[iBegin:iEnd] == eqStr {
			// text has the required equation number
			newText = text
			modified = false
		} else {
			// text has no or wrong equation number -> correct equation number
			newText = text[0:iBegin] + " " + eqStr + text[iEnd:]
			fmt.Println("      Equation number updated:", newText)
			modified = true
		}
	}
	return
}

// Update section documents with changed section or caption numbers,
// introducing missing element id's etc.
func updateSectionDocuments() {
	fmt.Printf("Change documents:\n")
	for iSectionFile, sectionFile := range BookStructure.SectionFiles {
		fmt.Printf("   %s\n", sectionFile.FileName)

		// First, check all internal links
		for iElement, element := range sectionFile.Elements {
			if element.StartTag == "<a" {
				if element.ID == "" {
					if element.Href != "" {
						// No ID defined, but internal link. Check whether Href target exists
						fileExists := false
						for _, sectionFile2 := range BookStructure.SectionFiles {
							if sectionFile2.FileName == element.NewText {
								fileExists = true
								break
							}
						}
						if !fileExists {
							fmt.Printf("      Internal link is wrong: <a href=\"%s\">%s<\\a>\n",
								element.Href, element.Text)
						}
					}

				} else {
					// Internal link; check that target is defined
					bookMark, present := Bookmarks[element.ID]
					if !present {
						fmt.Printf("      Internal link not resolved (wrong id?): <a href=\"%s\">%s<\\a>\n",
							element.Href, element.Text)
					} else {
						if bookMark.FileName != element.NewText ||
							(bookMark.Label != "" && bookMark.Label != element.Text) ||
							bookMark.Tooltip != element.Tooltip {

							// Either file name or label (text) or tooltip (title) was changed
							sectionFile.Elements[iElement].Modified = true

							if bookMark.Label != "" {
								sectionFile.Elements[iElement].Text = bookMark.Label
							}
							if sectionFile.FileName == bookMark.FileName {
								sectionFile.Elements[iElement].NewText = ""
							} else {
								sectionFile.Elements[iElement].NewText = bookMark.FileName
							}
							sectionFile.Elements[iElement].Tooltip = bookMark.Tooltip
							sectionFile.Modified = true

							fileName := sectionFile.Elements[iElement].NewText
							tooltip := sectionFile.Elements[iElement].Tooltip
							text := sectionFile.Elements[iElement].Text
							if tooltip == "" {
								fmt.Printf("      Link modified: <a href=\"%s#%s\">%s<\\a>\n", fileName, element.ID, text)
							} else {
								fmt.Printf("      Link modified: <a href=\"%s#%s\" title=\"%s\">%s<\\a>\n",
									fileName, element.ID, tooltip, text)
							}
						}
					}
				}
			}
		}

		// If file has to be modified, modify it
		if sectionFile.Modified || sectionFile.NewNav || sectionFile.UpdateNav {
			// Section document needs to be modified; move the file to the backup directory
			movedFileName := filepath.Join(BackupPath, sectionFile.FileName)
			err := os.Rename(sectionFile.FileName, movedFileName)
			if err != nil {
				log.Fatal(err)
			}

			// Generate the file newly
			updateOneSectionDocument(movedFileName, sectionFile, iSectionFile)
		}
	}
}

// Generate one section document newly
func updateOneSectionDocument(movedFileName string, sectionFile SectionFileType, iSectionFile int) {
	// Create section document file
	file, err := os.Create(sectionFile.FileName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Open old file and read it in byte vector old
	oldFile, err := ioutil.ReadFile(movedFileName)
	if err != nil {
		log.Fatal(err)
	}
	old := string(oldFile)

	// Initialize array indices
	iLast := 0   // Copy from this position in "old"
	iSearch := 0 // Search from this position in "old"
	iNext := 0   // Next index

	// Update navigation bar (if needed)
	if sectionFile.NewNav || sectionFile.UpdateNav {
		// New navigation bar, or update existing one; determine file names
		navFileToc := Configuration.TocFileName
		var navFilePrevious string
		var navFileNext string
		if iSectionFile > 0 {
			navFilePrevious = Configuration.SectionsFileNames[iSectionFile-1]
		} else {
			navFilePrevious = Configuration.CoverFileName
		}
		if iSectionFile < len(Configuration.SectionsFileNames)-1 {
			navFileNext = Configuration.SectionsFileNames[iSectionFile+1]
		} else {
			navFileNext = ""
		}

		if sectionFile.NewNav {
			// Introduce new navigation bar directly after <body>
			fmt.Printf("Generating new navigation bar \"%s\" directly after \"%s\" in file %s\n", beginNavBar, beginBody, sectionFile.FileName)
			iNext = strings.Index(old, beginBody)
			if iNext < 0 {
				fmt.Printf("Error: File \"%s\" does not contain \"%s\"\n", movedFileName, beginBody)
				os.Exit(1)
			}
			iNext = iNext + len(beginBody)
			fmt.Fprint(file, old[0:iNext])
			fmt.Fprintf(file, "\n")
			writeNavigationBar(file, navFilePrevious, navFileNext, navFileToc)
			iLast = iNext
			iSearch = iNext
		} else {
			// Navigation bar needs to be updated
			iNext = strings.Index(old, beginNavBar)
			if iNext < 0 {
				fmt.Printf("Unknown error (should not occur): File \"%s\" does not contain \"%s\"\n", movedFileName, beginNavBar)
				os.Exit(1)
			}
			// Make a copy of the actual file until <nav>, generate a new <nav>..</nav>
			fmt.Println("      Update navigation bar of file:", sectionFile.FileName)
			fmt.Fprint(file, old[0:iNext])
			writeNavigationBar(file, navFilePrevious, navFileNext, navFileToc)
			iSearch = iNext
			iNext = strings.Index(old[iSearch:], endNavBar)
			if iNext < 0 {
				fmt.Printf("Unknown error (should not occur): File \"%s\" contains \"%s\" but not \"%s\"\n", movedFileName, beginNavBar, endNavBar)
				os.Exit(1)
			}
			iLast = iSearch + iNext + len(endNavBar)
			iSearch = iLast
		}
	}

	// Loop over all modified elements
	for _, elem := range sectionFile.Elements {
		// Search next element in old document
		iNext = strings.Index(old[iSearch:], elem.StartTag)
		if iNext < 0 {
			fmt.Printf("Unknown error 1 (should not occur):\n"+
				"   Element \"%s ...>%s\" not found in file %s\n",
				elem.StartTag, elem.Text, movedFileName)
			os.Exit(1)
		}

		if elem.Modified || elem.NewID {
			// Element text or id was modified; needs to be newly generated

			// Copy previous file content until beginning of this element
			iNext = iSearch + iNext
			if elem.Modified && !elem.NewID {
				// If only the text was modified (but not the ID), replace only the text
				iSearch = iNext
				iNext = strings.Index(old[iSearch:], ">")
				if iNext == -1 {
					fmt.Printf("Unknown error 2 (should not occur):\n"+
						"   Element \"%s ...>%s\" not found in file %s\n",
						elem.StartTag, elem.Text, movedFileName)
					os.Exit(1)
				}
				iNext = iSearch + iNext + 1

				if elem.StartTag == "<a" {
					// Next element is a link that needs to be modified
					fmt.Fprint(file, old[iLast:iSearch])
					if elem.Tooltip == "" {
						fmt.Fprint(file, "<a href=\""+elem.NewText+"#"+elem.ID+"\">"+elem.Text)
					} else {
						fmt.Fprint(file, "<a href=\""+elem.NewText+"#"+elem.ID+"\" title=\""+elem.Tooltip+"\">"+elem.Text)
					}
				} else {
					// Next element is a section/caption element (like <h1>)
					fmt.Fprint(file, old[iLast:iNext])
					fmt.Fprint(file, elem.NewText)
				}
				iSearch = iNext
				iNext = strings.Index(old[iSearch:], elem.EndTag)
				if iNext == -1 {
					fmt.Printf("Unknown error 3 (should not occur):\n"+
						"   Element \"%s ...>%s%s\" not found in file %s\n",
						elem.StartTag, elem.Text, elem.EndTag, movedFileName)
					os.Exit(1)
				}
				iLast = iSearch + iNext
				iSearch = iLast + 1

			} else {
				// No ID was present and it needs to be newly introduced
				fmt.Fprint(file, old[iLast:iNext])
				fmt.Fprintf(file, "%s id=\"%s\">%s%s", elem.StartTag, elem.ID, elem.NewText, elem.EndTag)
				iSearch = iNext
				iNext = strings.Index(old[iSearch:], elem.EndTag)
				if iNext == -1 {
					fmt.Printf("Unknown error 4 (should not occur):\n"+
						"   Element \"%s ...>%s%s\" not found in file %s\n",
						elem.StartTag, elem.Text, elem.EndTag, movedFileName)
					os.Exit(1)
				}
				iLast = iSearch + iNext + len(elem.EndTag)
				iSearch = iLast + 1
			}

		} else {
			// Element not modified; continue search
			iSearch = iSearch + iNext + 1
		}
	}

	// Copy last part of file
	if iLast <= len(old) {
		fmt.Fprint(file, old[iLast:])
	}
}

// Write table of contents file
func writeContentsFile(oldFileName string, fileName string) {
	file, err := os.Create(fileName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	if oldFileName == "" {
		// No old contents version exists; generate it completely from scratch
		fmt.Println("Generate new Table-of-Contents file:", fileName)
		writeContentsHead(file)
		writeContentsStructure(file)
		writeContentsTail(file)

	} else {
		// Copy old contents version and replace Table-of-Contents part
		fmt.Println("Update Table-of-Contents file:", fileName)
		oldFile, err := ioutil.ReadFile(oldFileName)
		if err != nil {
			log.Fatal(err)
		}
		str := string(oldFile)
		i := strings.Index(str, beginTableOfContents)
		if i >= 1 {
			fmt.Fprint(file, str[0:i])
			writeContentsStructure(file)
			j := strings.Index(str[i:], endTableOfContents)
			if j >= 0 {
				fmt.Fprint(file, str[i+j+len(endTableOfContents)+1:])
			} else {
				fmt.Printf("Constructing default tail of file since \"%s\" not found on file %s\n", endTableOfContents, oldFileName)
				writeContentsTail(file)
			}

		} else {
			fmt.Printf("Generating Table-of-Contents file newly since \"%s\" not found on file %s\n", beginTableOfContents, oldFileName)
			writeContentsHead(file)
			writeContentsStructure(file)
			writeContentsTail(file)
		}
	}
}

func writeContentsHead(file *os.File) {
	fmt.Fprintln(file, "<!DOCTYPE html>")
	fmt.Fprintln(file, "<html lang=\"en\">")
	fmt.Fprintln(file, "<head>")
	fmt.Fprintln(file, "<style type=\"text/css\">")
	fmt.Fprintln(file, "  ol {margin: 0px 0 15px -20px; list-style-type: none;}")
	fmt.Fprintln(file, "  li {margin: 2px 0px 0px 0px;}")
	fmt.Fprintln(file, "  a  {text-decoration: none; color: green;}")
	fmt.Fprintln(file, "  a:hover {text-decoration: underline;}")
	fmt.Fprintln(file, "</style>")
	fmt.Fprintln(file, "</head>")
	fmt.Fprintln(file, "<body>")
}

func writeContentsTail(file *os.File) {
	fmt.Fprintln(file, "</body>")
	fmt.Fprintln(file, "</html>")
}

// Shorten caption string for "Table Of Contents
func shortenCaption(text string) string {
	const maxDisplayCharacters = 60 // Maximum number of characters to be showed for captions in Table-of-Contents
	if len(text) <= maxDisplayCharacters {
		return text
	} else {
		return text[0:maxDisplayCharacters-3] + "..."
	}
}

func writeContentsStructure(file *os.File) {
	fmt.Fprintln(file, beginTableOfContents)
	fmt.Fprintln(file, "<ol>")
	fmt.Fprintf(file, "<li><a href=\"%s\"><strong>Book Cover</strong></a></li>\n\n", BookStructure.CoverFileName)

	for _, h1 := range BookStructure.Sections {
		// h1 headings
		fmt.Fprintf(file, "\n<li><a href=\"%s#%s\"><strong>%s</strong></a>", h1.FileName, h1.ID, h1.Text)

		if len(h1.Sections) == 0 && len(h1.Captions) == 0 {
			fmt.Fprintf(file, "</li>\n")
		} else {
			if len(h1.Captions) > 0 {
				// caption or figcaption
				fmt.Fprintf(file, "\n    <ul class=\"tree\">\n")
				for _, caption := range h1.Captions {
					fmt.Fprintf(file, "    <li><a href=\"%s#%s\">%s</a></li>\n", caption.FileName, caption.ID, shortenCaption(caption.Text))
				}
				fmt.Fprintln(file, "    </ul>")
			}

			if len(h1.Sections) == 0 {
				fmt.Fprintln(file, "</li>")
			} else {
				// h2 headings
				fmt.Fprintf(file, "\n    <ol>\n")

				for _, h2 := range h1.Sections {
					fmt.Fprintf(file, "    <li><a href=\"%s#%s\">%s</a>", h2.FileName, h2.ID, h2.Text)

					if len(h2.Sections) == 0 && len(h2.Captions) == 0 {
						fmt.Fprintf(file, "</li>\n")
					} else {
						if len(h2.Captions) > 0 {
							// caption or figcaption
							fmt.Fprintf(file, "\n        <ul class=\"tree\">\n")
							for _, caption := range h2.Captions {
								fmt.Fprintf(file, "        <li><a href=\"%s#%s\">%s</a></li>\n", caption.FileName, caption.ID, shortenCaption(caption.Text))
							}
							fmt.Fprintln(file, "        </ul>")
						}

						if len(h2.Sections) == 0 {
							fmt.Fprintln(file, "    </li>")
						} else {
							// h3 headings
							fmt.Fprintf(file, "\n        <ul class=\"tree\">\n")
							for _, h3 := range h2.Sections {
								fmt.Fprintf(file, "        <li><a href=\"%s#%s\">%s</a>", h3.FileName, h3.ID, h3.Text)

								if len(h3.Sections) == 0 && len(h3.Captions) == 0 {
									fmt.Fprintf(file, "</li>\n")
								} else {
									if len(h3.Captions) > 0 {
										// caption or figcaption
										fmt.Fprintf(file, "\n            <ul class=\"tree\">\n")
										for _, caption := range h3.Captions {
											fmt.Fprintf(file, "            <li><a href=\"%s#%s\">%s</a></li>\n", caption.FileName, caption.ID, shortenCaption(caption.Text))
										}
										fmt.Fprintln(file, "            </ul>")
									}

									if len(h3.Sections) == 0 {
										fmt.Fprintln(file, "        </li>")
									} else {
										// h4 headings
										fmt.Fprintf(file, "\n            <ul class=\"tree\">\n")
										for _, h4 := range h3.Sections {
											fmt.Fprintf(file, "            <li><a href=\"%s#%s\">%s</a>", h4.FileName, h4.ID, h4.Text)

											if len(h4.Captions) == 0 {
												fmt.Fprintf(file, "</li>\n")
											} else {
												// caption or figcaption
												fmt.Fprintf(file, "\n                <ul class=\"tree\">\n")
												for _, caption := range h4.Captions {
													fmt.Fprintf(file, "                <li><a href=\"%s#%s\">%s</a></li>\n", caption.FileName, caption.ID, shortenCaption(caption.Text))
												}
												fmt.Fprintln(file, "                </ul></li>")
											}
										}
										fmt.Fprintln(file, "            </ul></li>")
									}
								}
							}
							fmt.Fprintln(file, "        </ul></li>")
						}
					}
				}
				fmt.Fprintln(file, "    </ol></li>")
			}
		}
	}
	fmt.Fprintln(file, "</ol>")
	fmt.Fprintln(file, endTableOfContents)
}

// Write or update navigation bar in one file
func updateNavigationBar(actualName, previousName, nextName, tocName string) {
	// Move actual file to backup directory and read it
	movedActualName := filepath.Join(BackupPath, actualName)
	err := os.Rename(actualName, movedActualName)
	if err != nil {
		log.Fatal(err)
	}
	actual, err := ioutil.ReadFile(movedActualName)
	if err != nil {
		log.Fatal(err)
	}
	str := string(actual)

	// Create actual file
	file, err := os.Create(actualName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Find <nav> in actual file
	i := strings.Index(str, beginNavBar)
	if i >= 0 {
		// Make a copy of the actual file until <nav>, generate a new <nav>..</nav> and copy the rest of the file
		fmt.Println("Update navigation bar of file:", actualName)
		fmt.Fprint(file, str[0:i])
		writeNavigationBar(file, previousName, nextName, tocName)
		j := strings.Index(str[i:], endNavBar)
		if j < 0 {
			fmt.Printf("File \"%s\" contains \"%s\" but not \"%s\"\n", movedActualName, beginNavBar, endNavBar)
			os.Exit(1)
		}
		fmt.Fprint(file, str[i+j+len(endNavBar)+1:])

	} else {
		// No <nav> present in file; generate it newly directly after <body>
		fmt.Printf("Generating new navigation bar \"%s\" directly after \"%s\" in file %s\n", beginNavBar, beginBody, actualName)
		i = strings.Index(str, beginBody)
		if i < 0 {
			fmt.Printf("File \"%s\" does not contain \"%s\"\n", movedActualName, beginBody)
			os.Exit(1)
		}
		fmt.Fprint(file, str[0:i])
		writeNavigationBar(file, previousName, nextName, tocName)
		fmt.Fprint(file, str[i+len(beginBody):])
	}
}

// Write navigation bar
func writeNavigationBar(file *os.File, previousName, nextName, tocName string) {
	fmt.Fprintln(file, "<nav><ul>")
	fmt.Fprintf(file, "  <li><a href=\"%s\">Table of Contents</a></li>\n", tocName)
	if previousName != "" {
		fmt.Fprintf(file, "  <li><a href=\"%s\">Previous</a></li>\n", previousName)
	}
	if nextName != "" {
		fmt.Fprintf(file, "  <li><a href=\"%s\">Next</a></li>\n", nextName)
	}
	fmt.Fprintln(file, "</ul></nav>")
}
