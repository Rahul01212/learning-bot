package checkstyle

import (
	"fmt"
	"gitlab.com/gitedulab/learning-bot/models"
	"gitlab.com/gitedulab/learning-bot/modules/settings"
	"io/ioutil"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	warnExp    = regexp.MustCompile(`^\[\w*\] `) // Matches the WARN label generated by checkstyle.
	pathExp    = regexp.MustCompile(`\: `)       // Matches the last colon before error description.
	lineColExp = regexp.MustCompile(`\:`)        // Matches a colon, for identifying line and columns in file.
	checkExp   = regexp.MustCompile(`\[\w*\]$`)  // Matches the check label at end of the line.
	descExp    = regexp.MustCompile(`\. \[`)     // Matches the full stop at the end of error description.
)

// GetIssues generates issues based on checkstyle's output.
func GetIssues(checkstyleOutput string, commitSHA string, path string, reportID int64) (issues []*models.Issue) {
	lines := strings.Split(checkstyleOutput, "\n")
	var wg sync.WaitGroup
	var mtx sync.Mutex
	var workers = make(chan struct{}, 3)
	for _, line := range lines {
		wg.Add(1)
		workers <- struct{}{}
		go func() {
			defer func() {
				wg.Done()
				<-workers
			}()
			ok, issue := parseLineIssue(line)
			if ok {
				issue.ReportID = reportID
				issue.SourceSnippet = getSnippet(issue.FilePath, issue.LineNumber, issue.ColumnNumber)
				issue.FilePath = strings.Split(issue.FilePath, path)[1] // remove /tmp/x from report
				mtx.Lock()                                              // prevent race condition
				issues = append(issues, issue)                          // Note: Order not preserved
				mtx.Unlock()
			}
		}()
	}
	wg.Wait()
	return issues
}

// getSnippet returns the lines of the code required for
// a snippet.
func getSnippet(path string, line int, col int) string {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		log.Printf("Unable to load file to generate snippet: %s\n", err)
		return ""
	}

	// Calculate which line to start the snippet from.
	var startLine int
	if line < settings.Config.CodeSnippetIncludeLines {
		startLine = line
	} else {
		startLine = line - settings.Config.CodeSnippetIncludeLines
	}
	var snippet string

	// Iterate through lines and add them
	lines := strings.Split(string(file), "\n")
	for i, lineStr := range lines {
		if i+1 >= startLine && i+1 <= line {
			snippet = snippet + lineStr
			if i+1 != line {
				snippet = snippet + "\n"
			}
		}
	}

	// Add column indicator
	if col != 0 {
		var space string
		for i := 0; i < col-1; i++ {
			space = space + " "
		}
		space = space + "^"
		snippet = snippet + "\n" + space
	}

	return snippet
}

// parseLineIssue parses a single line outputted by checkstyle into
// an Issue, boolean determines whether parsing the line is successful.
func parseLineIssue(line string) (ok bool, issue *models.Issue) {
	// TODO recover from panics here
	if string(warnExp.Find([]byte(line))) != "[WARN] " {
		return false, nil
	}
	issue = new(models.Issue)

	warn := warnExp.Split(line, -1)              // warn[1] is Without warning
	path := pathExp.Split(warn[1], -1)           // path[0] is path with line/col, path[1] is error description+checktype
	check := checkExp.Find([]byte(path[1]))      // [Checktype]
	checkName := string(check[1 : len(check)-1]) // Checktype without [ ]
	desc := descExp.Split(path[1], -1)           // desc[0] is plain description

	issue.CheckName = checkName
	issue.Description = fmt.Sprintf("%s.", desc[0])

	lineCol := lineColExp.Split(path[0], -1) // lineCol[0] is plain path, lineCol[1] is line number, lineCol[2] is column number
	if len(lineCol) > 0 {
		issue.FilePath = string(lineCol[0])
	}
	if len(lineCol) > 1 {
		i, err := strconv.Atoi(string(lineCol[1]))
		if err != nil {
			log.Fatalf("Cannot convert line number to integer: %s\n", err)
		}
		issue.LineNumber = i
	}
	if len(lineCol) > 2 {
		i, err := strconv.Atoi(string(lineCol[2]))
		if err != nil {
			log.Fatalf("Cannot convert column number to integer: %s\n", err)
		}
		issue.ColumnNumber = i
	}

	return true, issue
}
